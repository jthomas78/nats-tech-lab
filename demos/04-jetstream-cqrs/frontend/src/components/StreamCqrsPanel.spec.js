import PrimeVue from 'primevue/config'
import Select from 'primevue/select'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import AboutPanel from './AboutPanel.vue'
import StreamCqrsPanel from './StreamCqrsPanel.vue'
import VehiclePicker from './VehiclePicker.vue'
import RehydratePanel from './RehydratePanel.vue'
import LagLane from './LagLane.vue'
import EventLog from './EventLog.vue'

vi.mock('../rehydrate/api.js', async (importOriginal) => ({
  ...await importOriginal(),
  fetchBench: vi.fn().mockResolvedValue({
    kind: 'ok', stream: 'ODOMETER_BENCH', exists: true, events: 10_000, bytes: 840_000,
    sizes: [10000, 100000, 1000000],
    fixtures: [{ size: 10000, vehicle: 'bench-10k', events: 10000, snapSeq: 9950, tailLeft: 50 }],
  }),
}))

const writes = new Map([['truck-7', { vehicle: 'truck-7', plate: 'ABC', lastSeq: 8 }]])
const reads = new Map([['truck-7', { vehicle: 'truck-7', plate: 'ABC', lastSeq: 7 }]])
const mountPanel = () => mount(StreamCqrsPanel, {
  props: { vehicles: ['truck-7'], writes, reads, head: 99, messages: 42, bytes: 2048,
    logRows: [{ vehicle: 'truck-7', seq: 9 }],
    lags: { head: 99, writeSeq: 98, readSeq: 97 } },
  slots: { 'write-door': '<p data-testid="fake-door">door</p>' },
  global: { plugins: [PrimeVue] },
})
async function open(w, key) {
  await w.get(`[data-testid="lesson-01-tab-${key}"]`).trigger('click')
}

describe('lesson 01 has three rooms', () => {
  it('opens on Overview, reusing the whole explanation and exactly two references', () => {
    const w = mountPanel()
    expect(w.vm.tab).toBe('overview')
    expect(w.findComponent(AboutPanel).exists()).toBe(true)
    expect(w.find('[data-testid="fake-door"]').exists()).toBe(false)
    expect(w.findComponent(VehiclePicker).exists()).toBe(false)
    expect(w.get('[data-testid="lesson-summary"]').findAll('a').map(a => a.attributes('href'))).toEqual([
      'https://docs.nats.io/learn/jetstream/',
      'https://learn.microsoft.com/en-us/azure/architecture/patterns/cqrs',
    ])
    expect(w.findAll('[data-testid^="lesson-01-tab-"]').map(t => t.text())).toEqual(['Overview', 'Showcase', 'Performance'])
  })

  it('puts the picker and four causal groups inside Showcase', async () => {
    const w = mountPanel()
    await open(w, 'showcase')
    const showcase = w.get('[data-testid="showcase"]')
    expect(showcase.findComponent(VehiclePicker).exists()).toBe(true)
    expect(showcase.findAll('[data-group]').map(g => g.attributes('data-group'))).toEqual(['write', 'lag', 'kv', 'stream'])
    expect(showcase.find('[data-testid="fake-door"]').exists()).toBe(true)
    expect(showcase.text()).not.toContain('ODOMETER_BENCH')
    expect(showcase.text()).toContain('42 events · 2.0 KiB')
    expect(w.findComponent(LagLane).props('logLabel')).toContain('42 events · 2.0 KiB')
    for (const cmd of ['nats kv ls odometer-write', 'nats kv ls odometer-read', 'nats stream view ODOMETER', 'nats stream info ODOMETER']) {
      expect(showcase.text()).toContain(cmd)
    }
  })

  it('keeps both full key lists side by side, with the selected documents above them', async () => {
    const w = mountPanel()
    await open(w, 'showcase')
    w.findComponent(VehiclePicker).vm.$emit('update:modelValue', 'truck-7')
    await w.vm.$nextTick()
    for (const side of ['write', 'read']) {
      const col = w.get(`[data-testid="showcase-${side}"]`)
      const items = col.findAll(`[data-testid="bucket-${side}"], [data-testid="bucket-keys-${side}"]`)
      expect(items.map(i => i.attributes('data-testid'))).toEqual([`bucket-${side}`, `bucket-keys-${side}`])
      expect(items[0].text()).toContain('vehicle.truck-7')
    }
    expect(w.findComponent(LagLane).props('head')).toBe(9)
    expect(w.findComponent(EventLog).props('showVehicle')).toBe(false)
    await w.setProps({ vehicles: [] })
    expect(w.find('[data-testid="bucket-write"]').exists()).toBe(false)
    expect(w.find('[data-testid="bucket-keys-write"]').exists()).toBe(true)
  })

  // 04.7.18 (D13): Performance measures ODOMETER_BENCH. The live vehicle
  // picker belongs to Showcase, and the two selections never meet.
  it('puts Rehydrate alone under Performance, without a write door or sub-strip', async () => {
    const w = mountPanel()
    await open(w, 'performance')
    await flushPromises()
    expect(w.findComponent(RehydratePanel).exists()).toBe(true)
    expect(w.find('[data-testid="fake-door"]').exists()).toBe(false)
    expect(w.findAll('[role="tablist"]')).toHaveLength(1)
    expect(w.findComponent(RehydratePanel).findComponent(VehiclePicker).exists()).toBe(false)
    expect(w.findComponent(RehydratePanel).text()).not.toContain('truck-7')
  })

  // KeepAlive is the point: a reader who checks Showcase mid-experiment comes
  // back to the fixture they picked, not to an empty panel.
  it('keeps the fixture picked on Performance while you visit Showcase', async () => {
    const w = mountPanel()
    await open(w, 'performance')
    await flushPromises()
    const picker = w.findComponent(RehydratePanel).findComponent(Select)
    picker.vm.$emit('update:modelValue', 'bench-10k')
    await w.vm.$nextTick()
    expect(w.get('[data-testid="rehydrate-target"]').text()).toContain('bench-10k')

    await open(w, 'showcase')
    expect(w.find('[data-testid="fake-door"]').exists()).toBe(true)
    expect(w.findComponent(VehiclePicker).props('modelValue')).toBe(null)

    await open(w, 'performance')
    expect(w.get('[data-testid="rehydrate-target"]').text()).toContain('bench-10k')
  })
})
