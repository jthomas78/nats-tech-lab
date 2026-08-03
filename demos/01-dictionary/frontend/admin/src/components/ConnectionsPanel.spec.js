import PrimeVue from 'primevue/config'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ConnectionsPanel from './ConnectionsPanel.vue'

// BR-028 (Main-POC-Plan.md Phase 17c) — in the Admin UI, a connection's
// account should resolve to a friendly name (its tenant, or "DEFAULT")
// wherever the backend could determine one, falling back to the raw account
// NKey otherwise. The backend's resolution logic (nats_ops.go's
// tenantLabelsByAccount) already has its own Go test coverage; this file
// covers the frontend half of that same rule — the component must actually
// prefer tenantLabel when the API supplies it, not just carry the field
// through unrendered — plus the panel's filtering and detail-pane behavior.

vi.mock('../api', () => ({
  getNatsConnections: vi.fn(),
}))

import { getNatsConnections } from '../api'

const CONNECTIONS = [
  {
    cid: 1,
    name: 'refdata-service',
    type: 'nats',
    lang: 'go',
    version: '1.52.0',
    ip: '172.19.0.11',
    port: 48046,
    account: 'AA57B6BPPV3JQPCHSSCEALTMKL7YXGTT4WZI4CVVAHSO2TDQK6PYK2H6',
    tenantLabel: 'DEFAULT',
    rtt: '779µs',
    uptime: '1h56m',
    idle: '16s',
    inMsgs: 313,
    outMsgs: 841,
    subscriptions: 2,
    subscriptionsList: ['rpc.*.refdata.type.list.v1', '$SRV.STATS.refdata-service'],
  },
  {
    cid: 2,
    name: 'accounts-service',
    type: 'nats',
    lang: 'go',
    version: '1.52.0',
    ip: '172.19.0.10',
    port: 49001,
    account: 'AB56H4HBPU4ZVCTWCY6RZIVEAIE37CE7VKCQMJANMLO7YJZ2IELZAFJT',
    // No tenantLabel — the SYS-account gap (BR-028's "wherever possible").
    rtt: '962µs',
    uptime: '11h',
    idle: '11h',
    inMsgs: 0,
    outMsgs: 0,
    subscriptions: 9,
    subscriptionsList: [],
  },
  {
    cid: 3,
    name: '',
    type: 'websocket',
    lang: 'nats.ws',
    version: '3.4.0',
    ip: '192.168.65.1',
    port: 52520,
    account: 'AAFBCA52VV7PAJSYANHENP4XR7PPY2ACIJLVDMW2YLGV24VD6MWAPPNX',
    tenantLabel: 'acme',
    rtt: '1.76ms',
    uptime: '1m',
    idle: '1m',
    inMsgs: 0,
    outMsgs: 0,
    subscriptions: 0,
    subscriptionsList: [],
  },
]

function mountPanel() {
  return mount(ConnectionsPanel, {
    global: { plugins: [PrimeVue] },
  })
}

describe('ConnectionsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getNatsConnections.mockResolvedValue({ connections: CONNECTIONS })
  })

  it('BR-028: renders the resolved tenantLabel as a tag instead of the raw account NKey', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const labels = wrapper.findAll('.tenant-label').map((el) => el.text())
    expect(labels).toEqual(expect.arrayContaining(['DEFAULT', 'acme']))
  })

  it('BR-028: falls back to a truncated raw account NKey when no tenantLabel was resolved', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const raw = wrapper.findAll('.acct')
    expect(raw).toHaveLength(1)
    expect(raw[0].text()).toBe('AB56H4HBPU…')
    expect(raw[0].attributes('title')).toBe(CONNECTIONS[1].account)
  })

  it('shows the summary counts derived from the fetched connections', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.text()).toContain('3') // total
    // nats: refdata-service + accounts-service = 2; websocket: 1
    const values = wrapper.findAll('.summary-value').map((el) => el.text())
    expect(values[0]).toBe('3')
    expect(values[1]).toBe('2')
    expect(values[2]).toBe('1')
  })

  it('filters rows by tenantLabel text', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('.search-box input').setValue('acme')
    await flushPromises()

    const names = wrapper.findAll('tbody tr').map((row) => row.text())
    expect(names).toHaveLength(1)
    expect(names[0]).toContain('acme')
  })

  it('filters rows by subscription subject text', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('.search-box input').setValue('refdata.type.list')
    await flushPromises()

    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('tbody tr').text()).toContain('refdata-service')
  })

  it('filters rows by the websocket type chip', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const wsChip = wrapper.findAll('.chip').find((c) => c.text() === 'websocket')
    await wsChip.trigger('click')
    await flushPromises()

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('(unnamed)')
  })

  it('opens the detail pane on row click, showing the resolved tenantLabel, and closes on the close control', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.detail').exists()).toBe(false)

    await wrapper.findAll('tbody tr')[0].trigger('click')
    await flushPromises()

    const detail = wrapper.find('.detail')
    expect(detail.exists()).toBe(true)
    expect(detail.text()).toContain('DEFAULT')
    expect(detail.text()).toContain('refdata-service')
    expect(detail.findAll('.sub-item').map((el) => el.text())).toContain('rpc.*.refdata.type.list.v1')

    await detail.find('.close').trigger('click')
    await flushPromises()
    expect(wrapper.find('.detail').exists()).toBe(false)
  })

  it('shows an error message when the fetch fails', async () => {
    getNatsConnections.mockRejectedValue(new Error('boom'))
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.err-line').text()).toContain('boom')
  })
})
