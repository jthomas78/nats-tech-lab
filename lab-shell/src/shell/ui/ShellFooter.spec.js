import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'

import { SHELL } from '../shellKey.js'
import ShellFooter from './ShellFooter.vue'

const mountWith = (shell) =>
  mount(ShellFooter, {
    global: {
      provide: {
        [SHELL]: reactive({
          statuses: new Map(),
          contributions: { shellFooter: [] },
          registryError: null,
          registry: { revision: null, degraded: false },
          ...shell,
        }),
      },
      stubs: { PluginSlot: true },
    },
  })

describe('BR-AS22 — the registry degrades, it does not fail', () => {
  it('says nothing about the registry when it answered normally', () => {
    const w = mountWith({ registry: { revision: '50', degraded: false } })
    expect(w.find('[data-testid="registry-degraded"]').exists()).toBe(false)
    expect(w.find('[data-testid="registry-unavailable"]').exists()).toBe(false)
  })

  it('distinguishes a degraded answer from an empty catalog', () => {
    // An empty registry is a legitimate state (nothing curated yet). Only the
    // service's own degraded:true earns the notice.
    expect(mountWith({ registry: { revision: '0', degraded: false } })
      .find('[data-testid="registry-degraded"]').exists()).toBe(false)
    expect(mountWith({ registry: { revision: '0', degraded: true } })
      .find('[data-testid="registry-degraded"]').exists()).toBe(true)
  })

  it('distinguishes a degraded answer from no answer at all', () => {
    const w = mountWith({ registryError: { code: 'registry-unreachable', message: 'down' } })
    expect(w.find('[data-testid="registry-unavailable"]').exists()).toBe(true)
    expect(w.find('[data-testid="registry-degraded"]').exists()).toBe(false)
  })

  it('shows the revision and never the endpoint (BR-AS04)', () => {
    const w = mountWith({ registry: { revision: '50', degraded: true } })
    expect(w.text()).toContain('50')
    expect(w.text()).not.toContain('/api/')
  })
})
