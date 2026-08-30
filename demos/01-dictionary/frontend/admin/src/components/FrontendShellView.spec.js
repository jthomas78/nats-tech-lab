import PrimeVue from 'primevue/config'
import { createPinia, setActivePinia } from 'pinia'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import FrontendShellView from './FrontendShellView.vue'
import { useUiStore } from '../stores/ui'

// Phase 2 — the catalog and its write history are one nav item, "Frontend
// Shell", with two tabs. Two things this view exists to guarantee:
//
//   · Both readings of the registry are reachable from one rail entry, in a
//     fixed order — what is served now, then how it got that way.
//   · Only the active tab's panel is mounted. Each panel polls the registry
//     on its own onMounted, so mounting both would double the poll rate for
//     a panel nobody is looking at.

vi.mock('../api', () => ({
  getRegistryEntries: vi.fn().mockResolvedValue({ revision: 1, allowedOrigins: [], plugins: [] }),
  getRegistryAudit: vi.fn().mockResolvedValue([]),
}))

import { getRegistryAudit, getRegistryEntries } from '../api'

const mountView = () => mount(FrontendShellView, { global: { plugins: [PrimeVue] } })

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
})

describe('FrontendShellView', () => {
  it('offers exactly the two registry readings, Plugins first', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.findAll('[role="tab"]').map((t) => t.text())).toEqual(['Plugins', 'Registry Audit'])
  })

  it('mounts only the active tab, so the idle panel does not poll', async () => {
    mountView()
    await flushPromises()
    expect(getRegistryEntries).toHaveBeenCalled()
    expect(getRegistryAudit).not.toHaveBeenCalled()
  })

  it('reads the tab from the ui store, so the choice survives navigating away and back', async () => {
    const ui = useUiStore()
    ui.frontendShellTab = 'audit'
    mountView()
    await flushPromises()
    expect(getRegistryAudit).toHaveBeenCalled()
    expect(getRegistryEntries).not.toHaveBeenCalled()
  })
})
