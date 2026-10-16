import { createPinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import PulsePanel from './PulsePanel.vue'

// Phase 54c (decision 15, BR-077) — Pulse is marked unavailable until Phase
// 55. It used to count the whole live trace set from the KV feed. The trace
// store is now a search with a result limit, and a capped search is never
// shown as a total, so the histograms are gone. The Phase 44 explanation
// card and flow diagram stay.

const conn = vi.hoisted(() => ({ request: vi.fn(), subscribe: vi.fn() }))
vi.mock('../nats/usePlatformConnection.js', () => ({
  usePlatformConnection: () => conn,
}))
vi.mock('../api', () => ({ getKvBucketEntries: vi.fn() }))

import { getKvBucketEntries } from '../api'

function mountPanel() {
  return mount(PulsePanel, { global: { plugins: [createPinia()] } })
}

describe('PulsePanel (Phase 44; unavailable since Phase 54c, BR-077)', () => {
  it('says it is unavailable, why, and that Phase 55 brings it back', () => {
    const wrapper = mountPanel()
    const note = wrapper.find('[data-testid="pulse-unavailable"]')
    expect(note.exists()).toBe(true)
    expect(note.text()).toContain('unavailable until Phase 55')
    expect(note.text()).toContain('never shown as a total')
  })

  it('shows no request, error or latency numbers at all', () => {
    const wrapper = mountPanel()
    expect(wrapper.find('.pulse-row').exists()).toBe(false)
    expect(wrapper.find('.pulse-card').exists()).toBe(false)
  })

  it('reads no trace data: no KV snapshot, no subscription, no gateway search', () => {
    mountPanel()
    expect(getKvBucketEntries).not.toHaveBeenCalled()
    expect(conn.subscribe).not.toHaveBeenCalled()
    expect(conn.request).not.toHaveBeenCalled()
  })

  it('keeps the what-it-covers card and the animated flow diagram', () => {
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('What request/reply covers')
    expect(wrapper.text()).toContain('parentSpanId')
    expect(wrapper.find('.flow-card svg').exists()).toBe(true)
  })
})
