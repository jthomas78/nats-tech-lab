import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MessagesPanel from './MessagesPanel.vue'
import SubjectPath from './SubjectPath.vue'

// Phase 43c (BUSINESS_RULES-SHIPPING.md's BR-048). Specs derived from the
// rule: its own panel (not a RpcPanel tab), an evt/notify family filter
// defaulting to evt, the tenant named from the import remap, SubjectPath for
// subjects, and a row cap plus pause because evt.* volume exceeds the RPC
// volume RpcPanel was sized for.
//
// Live-update behavior isn't exercised here (usePlatformConnection is mocked
// inert), same as RpcPanel.spec.js/TraceWaterfall.spec.js — these cover the
// bootstrap-derived rendering. usePubsubFeed's own live path has its own
// specs in nats/usePubsubFeed.spec.js.

vi.mock('../api', () => ({ getKvBucketEntries: vi.fn() }))
vi.mock('../nats/usePlatformConnection.js', () => ({
  usePlatformConnection: () => ({ connected: ref(false), subscribe: vi.fn() }),
}))

import { getKvBucketEntries } from '../api'

const BASE = 1755000000000

function envelope(spanId, subject, extra = {}) {
  return {
    spanId,
    traceId: `t-${spanId}`,
    direction: 'publish',
    subject,
    payloadBytes: 128,
    payload: { ok: true },
    timestamp: new Date(BASE).toISOString(),
    ...extra,
  }
}
function kvEntry(tenant, span) {
  return { key: `_platform.msg.${span.spanId}`, op: 'PUT', revision: 1, value: { tenant, span } }
}

const EVT_ACME = envelope('s1', 'evt.acme.shipping.ship.SHIP-1.arrived')
const EVT_GLOBEX = envelope('s2', 'evt.globex.shipping.container.C-9.loaded')
const NOTIFY_ACME = envelope('s3', 'notify.acme.shipping.ship.changed')

async function mountPanel() {
  const wrapper = mount(MessagesPanel, { global: { plugins: [PrimeVue, createPinia()] } })
  await flushPromises()
  return wrapper
}

function subjects(wrapper) {
  return wrapper.findAllComponents(SubjectPath).map((c) => c.props('subject'))
}
function chip(wrapper, text) {
  return wrapper.findAll('button.chip').find((b) => b.text().trim() === text)
}

describe('MessagesPanel (Phase 43c, BR-048)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getKvBucketEntries.mockResolvedValue([
      kvEntry('acme', EVT_ACME),
      kvEntry('globex', EVT_GLOBEX),
      kvEntry('acme', NOTIFY_ACME),
    ])
  })

  it('reads the pubsub-messages bucket, not the trace bucket RpcPanel reads', async () => {
    await mountPanel()

    expect(getKvBucketEntries).toHaveBeenCalledWith('platform', 'pubsub-messages')
  })

  it('defaults the family filter to evt only, since notify.* is largely a fan-out of events already visible on the evt side', async () => {
    const wrapper = await mountPanel()

    // Newest first, same as RpcPanel's flat span list.
    expect(subjects(wrapper)).toEqual([
      'evt.globex.shipping.container.C-9.loaded',
      'evt.acme.shipping.ship.SHIP-1.arrived',
    ])
    expect(subjects(wrapper)).not.toContain('notify.acme.shipping.ship.changed')
  })

  it('filters by family via a toggle chip, mirroring RpcPanel\'s rpc/api control', async () => {
    const wrapper = await mountPanel()

    await chip(wrapper, 'notify').trigger('click')
    expect(subjects(wrapper)).toContain('notify.acme.shipping.ship.changed')

    await chip(wrapper, 'evt').trigger('click')
    expect(subjects(wrapper)).toEqual(['notify.acme.shipping.ship.changed'])
  })

  it('names the originating tenant per row, from the import remap rather than a coarse PLATFORM/TENANT split', async () => {
    const wrapper = await mountPanel()

    const tenants = wrapper.findAll('[data-testid="msg-tenant"]').map((n) => n.text())
    expect(tenants).toEqual(['globex', 'acme'])
  })

  it('renders each subject via SubjectPath so tokens read as chips', async () => {
    const wrapper = await mountPanel()

    expect(wrapper.findAllComponents(SubjectPath).length).toBeGreaterThan(0)
    expect(subjects(wrapper)).toContain('evt.acme.shipping.ship.SHIP-1.arrived')
  })

  it('offers a pause control that freezes the visible rows while the feed keeps running underneath', async () => {
    const wrapper = await mountPanel()
    expect(subjects(wrapper)).toHaveLength(2)

    await wrapper.find('button.pause-btn').trigger('click')
    expect(wrapper.find('button.pause-btn').text()).toContain('resume')

    // A row arriving while paused must not appear until resumed.
    wrapper.vm.upsertMessage('s4', { tenant: 'acme', span: envelope('s4', 'evt.acme.shipping.ship.SHIP-2.departed') })
    await flushPromises()
    expect(subjects(wrapper)).toHaveLength(2)

    await wrapper.find('button.pause-btn').trigger('click')
    await flushPromises()
    expect(subjects(wrapper)).toHaveLength(3)
  })

  it('caps rendered rows and says so, since evt.* volume exceeds the RPC volume RpcPanel was sized for', async () => {
    const wrapper = await mountPanel()
    const cap = wrapper.vm.MAX_ROWS

    for (let i = 0; i < cap + 5; i += 1) {
      wrapper.vm.upsertMessage(`bulk${i}`, {
        tenant: 'acme',
        span: envelope(`bulk${i}`, `evt.acme.shipping.ship.S${i}.arrived`),
      })
    }
    await flushPromises()

    expect(subjects(wrapper)).toHaveLength(cap)
    expect(wrapper.text()).toContain('most recent')
  })

  it('says the feed is best-effort rather than implying completeness it cannot deliver', async () => {
    // BR-047/ADR-047 A7: the emit is a fire-and-forget core-NATS publish, so
    // an envelope can be lost before it ever reaches the stream.
    const wrapper = await mountPanel()

    expect(wrapper.text().toLowerCase()).toContain('best-effort')
  })

  it('surfaces a failed bootstrap instead of rendering an empty panel that looks like silence', async () => {
    getKvBucketEntries.mockRejectedValue(new Error('nope'))

    const wrapper = await mountPanel()

    expect(wrapper.text()).toContain('failed to load')
  })
})
