import PrimeVue from 'primevue/config'
import Select from 'primevue/select'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import RehydratePanel from './RehydratePanel.vue'
import * as api from '../rehydrate/api.js'
import { formatCount } from '../view/format.js'

// The acceptance test for tasks 04.7.14 and 04.7.18.
//
// The old rules, and they still hold:
//
//   1. Nothing runs until a human presses a button. A rebuild from sequence 1
//      reads the whole log, so a panel that measured on mount would fire it
//      every time somebody clicked a tab.
//   2. Changing the target clears the numbers. One fixture's measurement
//      under another's name is a lie the screen tells silently.
//   3. A disagreement is never shown as a speed-up.
//
// What 04.7.18 adds:
//
//   4. ONE picker aims the measurement, and it sits on the same row as the
//      buttons it aims. Two controls aiming one measurement is the defect
//      this task exists to remove (D12).
//   5. The picker offers fixtures and nothing else. No vehicle from the
//      demo's own log, and no mention of ODOMETER anywhere on the tab (D13).
//   6. An unseeded size is shown, and cannot be chosen (D14). Each option
//      carries its own count (D15).
//   7. The tab opens on nothing picked, with Run off (D19).

const partial = {
  kind: 'ok',
  stream: 'ODOMETER_BENCH',
  subject: 'evt.odometer-bench.>',
  writeKv: 'odometer-bench-write',
  exists: true,
  events: 110_000,
  bytes: 9_122_611,
  sizes: [10000, 100000, 1000000],
  fixtures: [
    { size: 10000, vehicle: 'bench-10k', events: 10000, snapSeq: 9950, tailLeft: 50 },
    { size: 100000, vehicle: 'bench-100k', events: 100000, snapSeq: 109950, tailLeft: 50 },
  ],
}

// Mount, wait for the fixture read, and optionally pick a fixture the way the
// dropdown does.
const mountPanel = async (pick = 'bench-100k') => {
  const w = mount(RehydratePanel, { global: { plugins: [PrimeVue] } })
  await flushPromises()
  if (pick) {
    w.findComponent(Select).vm.$emit('update:modelValue', pick)
    await w.vm.$nextTick()
  }
  return w
}

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

beforeEach(() => {
  vi.restoreAllMocks()
  vi.spyOn(api, 'fetchBench').mockResolvedValue(partial)
})

describe('it never measures on its own', () => {
  it('asks the write side for nothing until the button is pressed', async () => {
    const both = vi.spyOn(api, 'fetchBoth')
    await mountPanel()
    await flushPromises()
    expect(both).not.toHaveBeenCalled()
  })

  it('runs both sides on the picked fixture', async () => {
    const both = vi.spyOn(api, 'fetchBoth').mockResolvedValue({ cold, warm })
    const w = await mountPanel()
    await w.get('[data-testid="rehydrate-run-both"]').trigger('click')
    await flushPromises()
    // The source is spelled out on every call. A default that silently
    // pointed the headline number at the wrong log would look like a result.
    expect(both).toHaveBeenCalledWith('bench-100k', { source: 'bench' })
    expect(w.get('[data-testid="rehydrate-verdict"]').text()).toContain('23x')
  })
})

describe('one picker, on the row it aims', () => {
  it('draws the picker in the same row as the Run buttons', async () => {
    const w = await mountPanel(null)
    const row = w.get('[data-testid="rehydrate-controls"]')
    expect(row.findComponent(Select).exists()).toBe(true)
    expect(row.find('[data-testid="rehydrate-run-both"]').exists()).toBe(true)
  })

  it('opens on nothing, with Run off until a fixture is picked', async () => {
    const w = await mountPanel(null)
    expect(w.findComponent(Select).props('modelValue')).toBe(null)
    expect(w.get('[data-testid="rehydrate-run-both"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-testid="rehydrate-needs-fixture"]').exists()).toBe(true)
    expect(w.get('[data-testid="rehydrate-cold"]').text()).toContain('Not run yet')

    w.findComponent(Select).vm.$emit('update:modelValue', 'bench-10k')
    await w.vm.$nextTick()
    expect(w.get('[data-testid="rehydrate-run-both"]').attributes('disabled')).toBeUndefined()
    expect(w.get('[data-testid="rehydrate-target"]').text()).toContain('ODOMETER_BENCH')
  })

  it('offers the fixtures, with their counts, and nothing else', async () => {
    const w = await mountPanel(null)
    const options = w.findComponent(Select).props('options')
    expect(options.map((o) => o.value)).toEqual(['bench-10k', 'bench-100k', 'bench-1m'])
    expect(options[1].count).toContain(formatCount(100_000))
  })

  // D14. A size nobody has seeded is on the list so the reader can see it is
  // missing, but it cannot be measured — there is nothing there to rebuild.
  it('shows an unseeded size without letting it be chosen', async () => {
    const w = await mountPanel(null)
    const options = w.findComponent(Select).props('options')
    expect(options.find((o) => o.value === 'bench-100k').disabled).toBe(false)
    const unseeded = options.find((o) => o.value === 'bench-1m')
    expect(unseeded.disabled).toBe(true)
    expect(unseeded.count).toContain('not seeded')
  })

  // D13. Everything on this tab measures ODOMETER_BENCH. A reader who sees
  // ODOMETER here has been told the wrong log is about to be read.
  it('never names the demo own log', async () => {
    const w = await mountPanel()
    const said = w.text() + JSON.stringify(w.findComponent(Select).props('options'))
    expect(said.split('ODOMETER_BENCH').join('')).not.toContain('ODOMETER')
  })

  // D16. Both of these aimed the measurement from somewhere other than the
  // picker, which is exactly the thing 04.7.18 removes.
  it('keeps no second way to aim', async () => {
    const w = await mountPanel()
    expect(w.findAll('[data-testid^="bench-measure-"]')).toHaveLength(0)
    expect(w.find('[data-testid="rehydrate-back-to-live"]').exists()).toBe(false)
  })
})

describe('a result never outlives its target', () => {
  it('clears both halves when the fixture changes', async () => {
    vi.spyOn(api, 'fetchBoth').mockResolvedValue({ cold, warm })
    const w = await mountPanel()
    await w.get('[data-testid="rehydrate-run-both"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="rehydrate-cold"]').text()).toContain('25')

    w.findComponent(Select).vm.$emit('update:modelValue', 'bench-10k')
    await w.vm.$nextTick()
    expect(w.get('[data-testid="rehydrate-cold"]').text()).toContain('Not run yet')
    expect(w.get('[data-testid="rehydrate-warm"]').text()).toContain('Not run yet')
  })

  it.each([true, false])('ignores a late response after changing fixture (both=%s)', async (both) => {
    let finish
    const pending = new Promise(resolve => { finish = resolve })
    vi.spyOn(api, both ? 'fetchBoth' : 'fetchRehydration').mockReturnValue(pending)
    const w = await mountPanel()
    if (both) await w.get('[data-testid="rehydrate-run-both"]').trigger('click')
    else await w.findAll('button').find(b => b.text() === 'Snapshot only').trigger('click')
    w.findComponent(Select).vm.$emit('update:modelValue', 'bench-10k')
    await w.vm.$nextTick()
    finish(both ? { cold, warm } : warm)
    await flushPromises()
    expect(w.get('[data-testid="rehydrate-target"]').text()).toContain('bench-10k')
    expect(w.get('[data-testid="rehydrate-warm"]').text()).toContain('Not run yet')
  })

  // A fixture that is purged away is no longer a thing to measure, and the
  // panel must not keep aiming at it.
  it('lets go of a fixture that is no longer seeded', async () => {
    const w = await mountPanel()
    w.findComponent({ name: 'BenchFixture' }).vm.$emit('state', { ...partial, fixtures: [] })
    await w.vm.$nextTick()
    expect(w.find('[data-testid="rehydrate-needs-fixture"]').exists()).toBe(true)
  })
})

describe('it never flatters the comparison', () => {
  it('shows no ratio when the two sides rebuilt different states', async () => {
    vi.spyOn(api, 'fetchBoth').mockResolvedValue({
      cold,
      warm: { ...warm, status: 'retired' },
    })
    const w = await mountPanel()
    await w.get('[data-testid="rehydrate-run-both"]').trigger('click')
    await flushPromises()

    const v = w.get('[data-testid="rehydrate-verdict"]')
    expect(v.text()).not.toContain('faster')
    expect(v.text()).toContain('means nothing')
  })

  it('reports a broken write side without a measurement', async () => {
    vi.spyOn(api, 'fetchBoth').mockResolvedValue({
      cold: { kind: 'broken', error: 'Unavailable', message: 'nats down' },
      warm,
    })
    const w = await mountPanel()
    await w.get('[data-testid="rehydrate-run-both"]').trigger('click')
    await flushPromises()

    expect(w.get('[data-testid="rehydrate-cold"]').text()).toContain('nats down')
    expect(w.get('[data-testid="rehydrate-verdict"]').text()).toContain('Run both sides')
  })

  // CLAUDE.md: "the snapshot is always stale". The panel says so with the
  // run's own number, not as a slogan.
  it('says how far the snapshot trailed on this run', async () => {
    vi.spyOn(api, 'fetchBoth').mockResolvedValue({ cold, warm })
    const w = await mountPanel()
    await w.get('[data-testid="rehydrate-run-both"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="rehydrate-stale"]').text()).toContain('trailed by 2')
  })
})
