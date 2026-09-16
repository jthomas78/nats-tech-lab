import PrimeVue from 'primevue/config'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import RehydratePanel from './RehydratePanel.vue'
import * as api from '../rehydrate/api.js'

// The acceptance test for task 04.7.14.
//
// Three things must hold, and all three are about honesty rather than layout:
//
//   1. Nothing runs until a human presses a button. A rebuild from sequence 1
//      reads the whole log for that vehicle, so a panel that measured on
//      mount would fire it every time somebody clicked a tab.
//   2. Switching the vehicle clears the numbers. V1's measurement under V2's
//      name is a lie the screen tells silently.
//   3. A disagreement is never shown as a speed-up. If the two sides rebuilt
//      different states there is no number, only the reason.

const mountPanel = (props = { vehicle: 'V1' }) =>
  mount(RehydratePanel, { props, global: { plugins: [PrimeVue] } })

const cold = {
  kind: 'ok',
  usedSnapshot: false,
  fromSeq: 1,
  eventsRead: 10001,
  lastSeq: 10003,
  elapsedMs: 25.4,
  status: 'registered',
  plate: 'ABC-123',
}
const warm = { ...cold, usedSnapshot: true, fromSeq: 10002, eventsRead: 2, elapsedMs: 1.1 }

beforeEach(() => vi.restoreAllMocks())

describe('it never measures on its own', () => {
  it('asks the write side for nothing until the button is pressed', async () => {
    const both = vi.spyOn(api, 'fetchBoth')
    mountPanel()
    await flushPromises()
    expect(both).not.toHaveBeenCalled()
  })

  it('runs both sides when the button is pressed', async () => {
    const both = vi.spyOn(api, 'fetchBoth').mockResolvedValue({ cold, warm })
    const w = mountPanel()
    await w.find('[data-testid="rehydrate-run-both"]').trigger('click')
    await flushPromises()
    // The source is spelled out on every call. A default that silently
    // pointed the headline number at the wrong log would look like a result.
    expect(both).toHaveBeenCalledWith('V1', { source: 'live' })
    expect(w.find('[data-testid="rehydrate-verdict"]').text()).toContain('23x')
  })
})

describe('it needs one vehicle', () => {
  it('refuses to offer a rebuild of all vehicles', () => {
    const w = mountPanel({ vehicle: null })
    expect(w.find('[data-testid="rehydrate-needs-vehicle"]').exists()).toBe(true)
    expect(w.find('[data-testid="rehydrate-run-both"]').exists()).toBe(false)
  })

  it('clears the numbers when the vehicle changes', async () => {
    vi.spyOn(api, 'fetchBoth').mockResolvedValue({ cold, warm })
    const w = mountPanel()
    await w.find('[data-testid="rehydrate-run-both"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="rehydrate-cold"]').text()).toContain('25')

    await w.setProps({ vehicle: 'V2' })
    expect(w.find('[data-testid="rehydrate-cold"]').text()).toContain('Not run yet')
  })
})

describe('it never flatters the comparison', () => {
  it('shows no ratio when the two sides rebuilt different states', async () => {
    vi.spyOn(api, 'fetchBoth').mockResolvedValue({
      cold,
      warm: { ...warm, status: 'retired' },
    })
    const w = mountPanel()
    await w.find('[data-testid="rehydrate-run-both"]').trigger('click')
    await flushPromises()

    const v = w.find('[data-testid="rehydrate-verdict"]')
    expect(v.text()).not.toContain('faster')
    expect(v.text()).toContain('means nothing')
  })

  it('reports a broken write side without a measurement', async () => {
    vi.spyOn(api, 'fetchBoth').mockResolvedValue({
      cold: { kind: 'broken', error: 'Unavailable', message: 'nats down' },
      warm,
    })
    const w = mountPanel()
    await w.find('[data-testid="rehydrate-run-both"]').trigger('click')
    await flushPromises()

    expect(w.find('[data-testid="rehydrate-cold"]').text()).toContain('nats down')
    expect(w.find('[data-testid="rehydrate-verdict"]').text()).toContain('Run both sides')
  })

  // CLAUDE.md: "the snapshot is always stale". The panel says so with the
  // run's own number, not as a slogan.
  it('says how far the snapshot trailed on this run', async () => {
    vi.spyOn(api, 'fetchBoth').mockResolvedValue({ cold, warm })
    const w = mountPanel()
    await w.find('[data-testid="rehydrate-run-both"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="rehydrate-stale"]').text()).toContain('trailed by 2')
  })
})

describe('it can measure a seeded fixture', () => {
  it('rebuilds the bench vehicle on the bench stream', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue({
      kind: 'ok',
      stream: 'ODOMETER_BENCH',
      exists: true,
      events: 1000000,
      bytes: 83000000,
      sizes: [10000, 100000, 1000000],
      fixtures: [
        { size: 1000000, vehicle: 'bench-1m', events: 1000000, snapSeq: 999950, tailLeft: 50 },
      ],
      elapsedMs: 2195,
    })
    const both = vi.spyOn(api, 'fetchBoth').mockResolvedValue({ cold, warm })
    const w = mountPanel({ vehicle: null })
    await flushPromises()

    // With no vehicle picked there is nothing to rebuild -- until a fixture
    // is handed over. The bench vehicles live in another bucket, so they
    // never appear in the picker.
    expect(w.find('[data-testid="rehydrate-needs-vehicle"]').exists()).toBe(true)

    await w.find('[data-testid="bench-measure-bench-1m"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="rehydrate-target"]').text()).toContain('ODOMETER_BENCH')

    await w.find('[data-testid="rehydrate-run-both"]').trigger('click')
    await flushPromises()
    expect(both).toHaveBeenCalledWith('bench-1m', { source: 'bench' })
  })
})
