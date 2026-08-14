import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ServicesPanel from './ServicesPanel.vue'

// BR-028 (Main-POC-Plan.md Phase 17c) — same rule as ConnectionsPanel.spec.js,
// covering the Services panel's half: a service instance's tenant metadata
// (browserrpc.Adapter's micro.Config.Metadata, threaded through from
// tenant.go) must actually render as a tag, not just be carried through the
// API response unrendered. refdata-service has no tenant (it's a single
// global registration, not tenant-scoped), which doubles as coverage that
// the tag is conditional, not always shown.

vi.mock('../api', () => ({
  getNatsServices: vi.fn(),
}))

import { getNatsServices } from '../api'

const SERVICES = [
  {
    name: 'refdata-service',
    version: '1.0.0',
    instances: [
      {
        id: '4pMPXeAoTGXnykI0PGvxK0',
        started: '2026-08-01T17:43:47.140Z',
        // No metadata — refdata-service isn't tenant-scoped.
        endpoints: [
          { name: 'type-list', subject: 'rpc.*.refdata.type.list.v1', queueGroup: 'q', numRequests: 4, numErrors: 0, averageProcessingTimeMs: 21 },
          { name: 'item-get', subject: 'rpc.*.refdata.item.get.v1', queueGroup: 'q', numRequests: 0, numErrors: 0, averageProcessingTimeMs: 0 },
        ],
      },
    ],
  },
  {
    name: 'shipping-service',
    version: '1.0.0',
    instances: [
      {
        id: 'N7F3pbZ2cQtSJvMawjT4P9',
        started: '2026-08-02T08:39:21.000Z',
        metadata: { tenant: 'acme' },
        endpoints: [
          { name: 'ship-list', subject: 'api.*.shipping.ship.list.v1', queueGroup: 'q', numRequests: 3, numErrors: 1, averageProcessingTimeMs: 5, lastError: 'boom' },
        ],
      },
    ],
  },
]

function mountPanel() {
  return mount(ServicesPanel)
}

describe('ServicesPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getNatsServices.mockResolvedValue({ services: SERVICES })
  })

  it('shows the summary totals', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const values = wrapper.findAll('.summary-value').map((el) => el.text())
    expect(values[0]).toBe('2') // services
    expect(values[1]).toBe('2') // instances
    expect(values[2]).toBe('3') // endpoints (2 + 1)
    expect(values[3]).toContain('7') // requests: 4 + 0 + 3
    expect(values[3]).toContain('1') // errors: 0 + 0 + 1
  })

  it('starts with every card collapsed — expansion is click-only', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('.svc-card')).not.toHaveLength(0)
    expect(wrapper.findAll('.svc-card.expanded')).toHaveLength(0)
  })

  it('does not re-open a card on the refresh poll after the user collapses it', async () => {
    // The regression this replaces: refresh() used to auto-expand the first
    // card whenever `expanded` was empty, and refresh() runs on a 10s poll —
    // so collapsing every card silently re-opened one on the next tick.
    vi.useFakeTimers()
    try {
      const wrapper = mountPanel()
      await flushPromises()

      await wrapper.findAll('.svc-head')[0].trigger('click')
      await flushPromises()
      expect(wrapper.findAll('.svc-card.expanded')).toHaveLength(1)

      await wrapper.findAll('.svc-head')[0].trigger('click')
      await flushPromises()
      expect(wrapper.findAll('.svc-card.expanded')).toHaveLength(0)

      await vi.advanceTimersByTimeAsync(30000)
      await flushPromises()

      expect(wrapper.findAll('.svc-card.expanded')).toHaveLength(0)
    } finally {
      vi.useRealTimers()
    }
  })

  it('BR-028: shows the tenant tag on an instance with metadata, and omits it when absent', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // Nothing expands on its own, so both cards are opened explicitly here —
    // refdata-service too, since its instance-head has to be rendered before
    // we can assert it carries no tag.
    const headers = wrapper.findAll('.svc-head')
    await headers[0].trigger('click')
    await headers[1].trigger('click')
    await flushPromises()

    const tags = wrapper.findAll('.tenant-tag')
    expect(tags).toHaveLength(1)
    expect(tags[0].text()).toBe('acme')

    // refdata-service's instance-head must not render a tag at all.
    const refdataCard = wrapper.findAll('.svc-card')[0]
    expect(refdataCard.find('.tenant-tag').exists()).toBe(false)
  })

  it('toggles a service card open and closed on header click', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const shippingHeader = wrapper.findAll('.svc-head')[1]
    expect(wrapper.findAll('.svc-card')[1].classes()).not.toContain('expanded')

    await shippingHeader.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.svc-card')[1].classes()).toContain('expanded')

    await shippingHeader.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.svc-card')[1].classes()).not.toContain('expanded')
  })

  it('marks an endpoint error count and shows its last error', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.findAll('.svc-head')[1].trigger('click')
    await flushPromises()

    const errCell = wrapper.find('.svc-card:nth-child(2) td.errv')
    expect(errCell.exists()).toBe(true)
    expect(errCell.text()).toBe('1')
    expect(wrapper.find('.last-error').text()).toContain('boom')
  })

  it('shows a loading spinner while the initial fetch is in flight, then hides it once loaded', async () => {
    let resolveFetch
    getNatsServices.mockReturnValue(new Promise((resolve) => { resolveFetch = resolve }))
    const wrapper = mountPanel()
    await nextTick()

    expect(wrapper.find('.loading-line').exists()).toBe(true)
    expect(wrapper.find('.spinner').exists()).toBe(true)
    // Nothing else should render underneath the spinner while still loading.
    expect(wrapper.find('.empty-line').exists()).toBe(false)
    expect(wrapper.find('.err-line').exists()).toBe(false)

    resolveFetch({ services: SERVICES })
    await flushPromises()

    expect(wrapper.find('.loading-line').exists()).toBe(false)
  })

  it('shows an empty-state message when no services are registered', async () => {
    getNatsServices.mockResolvedValue({ services: [] })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.empty-line').text()).toContain('No micro-registered services')
  })

  it('shows an error message when the fetch fails', async () => {
    getNatsServices.mockRejectedValue(new Error('boom'))
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.err-line').text()).toContain('boom')
  })
})
