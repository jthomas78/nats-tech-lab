import PrimeVue from 'primevue/config'
import { createPinia, setActivePinia } from 'pinia'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import FrontendShellView from './FrontendShellView.vue'
import { useUiStore } from '../stores/ui'

// Phase 2 — the catalog and its write history are one nav item, "Frontend
// Shell", with tabs. Phase 7b added a third: who is trusted to sign what.
// Two things this view exists to guarantee:
//
//   · Every reading of the registry is reachable from one rail entry, in a
//     fixed order — what is served now, who may sign it, then how it got
//     that way.
//   · Only the active tab's panel is mounted. Each panel polls the registry
//     on its own onMounted, so mounting both would double the poll rate for
//     a panel nobody is looking at.

vi.mock('../api', () => ({
  getRegistryEntries: vi.fn().mockResolvedValue({ revision: 1, allowedOrigins: [], plugins: [] }),
  getRegistryAudit: vi.fn().mockResolvedValue([]),
  getRegistryPublishers: vi.fn().mockResolvedValue({ revision: 1, publishers: [] }),
  upsertPublisher: vi.fn(),
  addPublisherKey: vi.fn(),
  setPublisherKeyState: vi.fn(),
  transferPlugin: vi.fn(),
}))

import { getRegistryAudit, getRegistryEntries, getRegistryPublishers } from '../api'

const mountView = () => mount(FrontendShellView, { global: { plugins: [PrimeVue] } })

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
})

describe('FrontendShellView', () => {
  it('offers exactly the three registry readings, Plugins first', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.findAll('[role="tab"]').map((t) => t.text())).toEqual([
      'Plugins',
      'Publishers',
      'Registry Audit',
    ])
  })

  it('mounts only the active tab, so the idle panel does not poll', async () => {
    mountView()
    await flushPromises()
    expect(getRegistryEntries).toHaveBeenCalled()
    expect(getRegistryAudit).not.toHaveBeenCalled()
    expect(getRegistryPublishers).not.toHaveBeenCalled()
  })

  it('reads the tab from the ui store, so the choice survives navigating away and back', async () => {
    const ui = useUiStore()
    ui.frontendShellTab = 'audit'
    mountView()
    await flushPromises()
    expect(getRegistryAudit).toHaveBeenCalled()
    expect(getRegistryEntries).not.toHaveBeenCalled()
  })

  it('mounts the trust table only when its own tab is the active one', async () => {
    const ui = useUiStore()
    ui.frontendShellTab = 'publishers'
    mountView()
    await flushPromises()
    expect(getRegistryPublishers).toHaveBeenCalled()
    expect(getRegistryEntries).not.toHaveBeenCalled()
    expect(getRegistryAudit).not.toHaveBeenCalled()
  })
})
