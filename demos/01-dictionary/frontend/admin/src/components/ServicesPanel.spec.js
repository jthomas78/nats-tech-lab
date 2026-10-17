import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ServicesPanel from './ServicesPanel.vue'
import { SERVICE_LIST_SUBJECT } from '../nats/serviceList.js'

// Phase 54c (decision 17, BR-072) — the Services panel reads the live
// service list over NATS (api._platform.observability.service.list.v1), not
// GET /api/nats/services, which Phase 54d retired. There is no REST fallback. The snapshot carries the
// account each service was found in, readiness, and whether discovery was
// complete; the panel shows all three and never shows a failed or partial
// discovery as an empty success.
//
// BR-028 (Phase 17c) still holds: a service instance's tenant metadata
// (browserrpc.Adapter's micro.Config.Metadata) renders as a tag. refdata-
// service has no tenant, which doubles as coverage that the tag is
// conditional.

const conn = vi.hoisted(() => ({ request: vi.fn(), connected: null }))
vi.mock('../nats/usePlatformConnection.js', () => ({
  usePlatformConnection: () => conn,
}))
import * as api from '../api'

const OBSERVED = '2026-10-16T09:14:05.000Z'

function snapshot(over = {}) {
  return {
    schema: 'lab.servicelist.v1',
    observedAt: OBSERVED,
    windowMs: 500,
    complete: true,
    truncated: false,
    accounts: [
      { name: 'PLATFORM', status: 'ok' },
      { name: 'acme', status: 'ok' },
    ],
    services: [
      {
        name: 'refdata-service',
        account: 'PLATFORM',
        readiness: 'ready',
        instances: [
          {
            id: '4pMPXeAoTGXnykI0PGvxK0',
            version: '1.0.0',
            started: '2026-08-01T17:43:47.140Z',
            endpoints: [
              { name: 'type-list', subject: 'rpc.*.refdata.type.list.v1', queueGroup: 'q', numRequests: 4, numErrors: 0, averageProcessingTimeMs: 21 },
              { name: 'item-get', subject: 'rpc.*.refdata.item.get.v1', queueGroup: 'q', numRequests: 0, numErrors: 0, averageProcessingTimeMs: 0 },
            ],
          },
        ],
      },
      {
        name: 'shipping-service',
        account: 'PLATFORM',
        readiness: 'not-ready',
        instances: [
          {
            id: 'P1atf0rmShipInstance01',
            version: '1.2.0',
            started: '2026-08-02T08:39:20.000Z',
            endpoints: [
              { name: 'health', subject: 'rpc._platform.health.shipping-service.ready.v1', queueGroup: 'q', numRequests: 0, numErrors: 0, averageProcessingTimeMs: 0 },
            ],
          },
        ],
      },
      {
        name: 'shipping-service',
        account: 'acme',
        readiness: 'unknown',
        instances: [
          {
            id: 'N7F3pbZ2cQtSJvMawjT4P9',
            version: '1.2.0',
            started: '2026-08-02T08:39:21.000Z',
            metadata: { tenant: 'acme' },
            endpoints: [
              { name: 'ship-list', subject: 'api.*.shipping.ship.list.v1', queueGroup: 'q', numRequests: 3, numErrors: 1, averageProcessingTimeMs: 5, lastError: 'boom' },
            ],
          },
        ],
      },
    ],
    ...over,
  }
}

function mountPanel() {
  return mount(ServicesPanel)
}

describe('ServicesPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    conn.connected = ref(true)
    conn.request.mockResolvedValue(snapshot())
  })

  it('BR-072: asks the NATS service list with an empty body, and the REST route is gone', async () => {
    mountPanel()
    await flushPromises()

    expect(SERVICE_LIST_SUBJECT).toBe('api._platform.observability.service.list.v1')
    expect(conn.request).toHaveBeenCalledWith(SERVICE_LIST_SUBJECT, {})
    expect(api.getNatsServices).toBeUndefined()
  })

  it('shows the summary totals', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const values = wrapper.findAll('.summary-value').map((el) => el.text())
    expect(values[0]).toBe('3') // services: one name in two accounts is two
    expect(values[1]).toBe('3') // instances
    expect(values[2]).toBe('4') // endpoints (2 + 1 + 1)
    expect(values[3]).toContain('7') // requests: 4 + 0 + 0 + 3
    expect(values[3]).toContain('1') // errors
  })

  it('BR-072: keys a card by account and name, so one name in two accounts is two cards', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const cards = wrapper.findAll('.svc-card')
    expect(cards).toHaveLength(3)
    expect(cards[1].find('.svc-name').text()).toBe('shipping-service')
    expect(cards[2].find('.svc-name').text()).toBe('shipping-service')
    expect(cards[1].find('.account-tag').text()).toBe('PLATFORM')
    expect(cards[2].find('.account-tag').text()).toBe('acme')

    await cards[2].find('.svc-head').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.svc-card')[1].classes()).not.toContain('expanded')
    expect(wrapper.findAll('.svc-card')[2].classes()).toContain('expanded')
  })

  it('BR-072: shows readiness as ready, not ready or unknown — never infers it from a reply', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const badges = wrapper.findAll('[data-testid="svc-readiness"]').map((el) => el.text())
    expect(badges).toEqual(['ready', 'not ready', 'readiness unknown'])
    const dots = wrapper.findAll('.svc-head .dot').map((el) => el.classes())
    expect(dots[0]).toContain('ok')
    expect(dots[1]).toContain('bad')
    expect(dots[2]).toContain('unknown')
  })

  it('shows the instance version on the card', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('.svc-version').map((el) => el.text())).toEqual(['v1.0.0', 'v1.2.0', 'v1.2.0'])
  })

  it('BR-072: says when it was observed and how long the window was', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const line = wrapper.find('[data-testid="svc-observed"]')
    expect(line.text()).toContain('500 ms')
    expect(line.text()).toContain('no reply means not observed')
  })

  it('starts with every card collapsed — expansion is click-only', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('.svc-card')).not.toHaveLength(0)
    expect(wrapper.findAll('.svc-card.expanded')).toHaveLength(0)
  })

  it('does not re-open a card on the refresh poll after the user collapses it', async () => {
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

      expect(conn.request.mock.calls.length).toBeGreaterThan(1)
      expect(wrapper.findAll('.svc-card.expanded')).toHaveLength(0)
    } finally {
      vi.useRealTimers()
    }
  })

  it('BR-028: shows the tenant tag on an instance with metadata, and omits it when absent', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const headers = wrapper.findAll('.svc-head')
    await headers[0].trigger('click')
    await headers[2].trigger('click')
    await flushPromises()

    const tags = wrapper.findAll('.tenant-tag')
    expect(tags).toHaveLength(1)
    expect(tags[0].text()).toBe('acme')
    expect(wrapper.findAll('.svc-card')[0].find('.tenant-tag').exists()).toBe(false)
  })

  it('marks an endpoint error count and shows its last error', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.findAll('.svc-head')[2].trigger('click')
    await flushPromises()

    const errCell = wrapper.findAll('.svc-card')[2].find('td.errv')
    expect(errCell.exists()).toBe(true)
    expect(errCell.text()).toBe('1')
    expect(wrapper.find('.last-error').text()).toContain('boom')
  })

  it('BR-072: shows a partial discovery and its errors, and each failed account', async () => {
    conn.request.mockResolvedValue(
      snapshot({
        complete: false,
        errors: ['tenant list unavailable: tenant accounts were not queried'],
        accounts: [
          { name: 'PLATFORM', status: 'ok' },
          { name: 'acme', status: 'error', error: 'discovery request failed' },
        ],
      }),
    )
    const wrapper = mountPanel()
    await flushPromises()

    const partial = wrapper.find('[data-testid="svc-partial"]')
    expect(partial.exists()).toBe(true)
    expect(partial.text()).toContain('Partial result')
    expect(partial.text()).toContain('tenant list unavailable')
    expect(partial.text()).toContain('acme: discovery request failed')
  })

  it('BR-072: says when the reply was truncated', async () => {
    conn.request.mockResolvedValue(
      snapshot({ complete: false, truncated: true, errors: ['truncated at 200 instances'] }),
    )
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('[data-testid="svc-partial"]').text()).toContain('truncated at 200 instances')
  })

  it('BR-072: never shows a failed discovery as an empty success', async () => {
    conn.request.mockResolvedValue(
      snapshot({
        complete: false,
        services: [],
        accounts: [{ name: 'PLATFORM', status: 'error', error: 'discovery request failed' }],
      }),
    )
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.empty-line').exists()).toBe(false)
    expect(wrapper.find('[data-testid="svc-partial"]').text()).toContain('PLATFORM: discovery request failed')
  })

  it('shows a loading spinner while the initial request is in flight, then hides it once loaded', async () => {
    let resolveRequest
    conn.request.mockReturnValue(new Promise((resolve) => { resolveRequest = resolve }))
    const wrapper = mountPanel()
    await nextTick()

    expect(wrapper.find('.loading-line').exists()).toBe(true)
    expect(wrapper.find('.spinner').exists()).toBe(true)
    expect(wrapper.find('.empty-line').exists()).toBe(false)
    expect(wrapper.find('.err-line').exists()).toBe(false)

    resolveRequest(snapshot())
    await flushPromises()

    expect(wrapper.find('.loading-line').exists()).toBe(false)
  })

  it('shows an empty-state message only for a complete discovery with no services', async () => {
    conn.request.mockResolvedValue(snapshot({ services: [] }))
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.empty-line').text()).toContain('No micro service answered')
    expect(wrapper.find('[data-testid="svc-partial"]').exists()).toBe(false)
  })

  it('shows an error when the request fails, and keeps the last snapshot', async () => {
    vi.useFakeTimers()
    try {
      const wrapper = mountPanel()
      await flushPromises()
      expect(wrapper.findAll('.svc-card')).toHaveLength(3)

      conn.request.mockRejectedValue(new Error('timeout'))
      await vi.advanceTimersByTimeAsync(10000)
      await flushPromises()

      expect(wrapper.find('.err-line').text()).toContain('timeout')
      expect(wrapper.findAll('.svc-card')).toHaveLength(3)
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not ask while NATS is disconnected, and says so', async () => {
    conn.connected = ref(false)
    const wrapper = mountPanel()
    await flushPromises()

    expect(conn.request).not.toHaveBeenCalled()
    expect(wrapper.find('.err-line').text()).toContain('Not connected to NATS')
  })
})
