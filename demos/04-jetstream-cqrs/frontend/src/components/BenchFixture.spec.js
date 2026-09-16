import PrimeVue from 'primevue/config'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import BenchFixture from './BenchFixture.vue'
import * as api from '../rehydrate/api.js'
import { formatCount } from '../view/format.js'

// The acceptance test for Stream information, task 04.7.18 (D17, D18).
//
// The group has one job: say what is in ODOMETER_BENCH, and put it there.
// Five things must hold:
//
//   1. The stream is named ONCE, with its count and its bytes (D15, and the
//      standing rule from 2026-09-16 — a length never travels alone).
//   2. There is ONE seed button, not three. Three buttons made the reader
//      choose before they had any reason to (D18).
//   3. The table always shows all three sizes. A size nobody has seeded is a
//      row that says so, not a row that is missing.
//   4. A seed in flight reports how far it has got and locks the button. One
//      press writes 1 110 000 events; a screen that went quiet for that long
//      would look broken.
//   5. The price is on screen BEFORE the press, in events and in bytes.
//
// And one thing must be gone: this group no longer hands a target to the
// measurement (D16). The picker does that now.

const mountBench = () => mount(BenchFixture, { global: { plugins: [PrimeVue] } })

// 110 000 events is 10k + 100k seeded, 1m not. 9 122 611 bytes is 8.7 MiB.
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
  elapsedMs: 240,
}

const empty = { ...partial, exists: false, events: 0, bytes: 0, fixtures: [], elapsedMs: 0 }

// How many times a string appears in the whole rendered panel.
const times = (w, needle) => w.text().split(needle).length - 1

beforeEach(() => vi.restoreAllMocks())

describe('the header is the whole report', () => {
  it('names the stream, its count and its bytes exactly once', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(partial)
    const w = mountBench()
    await flushPromises()

    const head = w.get('[data-testid="bench-holds"]').text()
    expect(head).toContain('Stream information')
    expect(head).toContain('ODOMETER_BENCH')
    // formatCount groups with a non-breaking thin space, not a plain one.
    // Spelling it by hand here would pass for the wrong reason.
    expect(head).toContain(formatCount(110_000))
    expect(head).toContain('8.7 MiB')

    expect(times(w, 'ODOMETER_BENCH')).toBe(1)
    expect(times(w, '8.7 MiB')).toBe(1)
  })

  it('reports an empty fixture in the same place, bytes and all', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    const w = mountBench()
    await flushPromises()
    const head = w.get('[data-testid="bench-holds"]').text()
    expect(head).toContain('0 events')
    expect(head).toContain('0 B')
  })

  // D13. The Performance tab measures ODOMETER_BENCH and nothing else, and a
  // reader who sees ODOMETER on it has been told the wrong log is at risk.
  it('never names the demo own log', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(partial)
    const w = mountBench()
    await flushPromises()
    expect(w.text().split('ODOMETER_BENCH').join('')).not.toContain('ODOMETER')
  })
})

describe('one button seeds all three sizes', () => {
  it('offers one seed button, not one per size', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    const w = mountBench()
    await flushPromises()
    expect(w.find('[data-testid="bench-seed"]').exists()).toBe(true)
    expect(w.findAll('[data-testid^="bench-size-"]')).toHaveLength(0)
  })

  it('states the price in events and bytes before the press', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    const w = mountBench()
    await flushPromises()
    const cost = w.get('[data-testid="bench-cost"]').text()
    expect(cost).toContain(formatCount(1_110_000))
    expect(cost).toMatch(/MiB|GiB/)
    expect(cost).toContain('replaces')
  })

  it('seeds every size, in order, on one press', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    const seed = vi.spyOn(api, 'seedFixture').mockResolvedValue(partial)
    const w = mountBench()
    await flushPromises()
    await w.get('[data-testid="bench-seed"]').trigger('click')
    await flushPromises()
    expect(seed.mock.calls).toEqual([[10000], [100000], [1000000]])
  })

  it('says how far it has got, and locks the button while it runs', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    let finish
    vi.spyOn(api, 'seedFixture').mockReturnValue(new Promise((r) => { finish = r }))
    const w = mountBench()
    await flushPromises()
    await w.get('[data-testid="bench-seed"]').trigger('click')
    await flushPromises()

    const said = w.get('[data-testid="bench-progress"]').text()
    expect(said).toContain('bench-10k')
    expect(said).toContain('1 of 3')
    expect(said).toContain(formatCount(1_110_000))
    expect(w.get('[data-testid="bench-seed"]').attributes('disabled')).toBeDefined()

    finish(partial)
    await flushPromises()
  })
})

describe('the table always shows all three sizes', () => {
  it('draws a row per size, seeded or not', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(partial)
    const w = mountBench()
    await flushPromises()
    const rows = w.findAll('[data-testid^="bench-row-"]')
    expect(rows.map((r) => r.attributes('data-testid'))).toEqual([
      'bench-row-bench-10k',
      'bench-row-bench-100k',
      'bench-row-bench-1m',
    ])
    expect(w.get('[data-testid="bench-row-bench-10k"]').text()).toContain(`seq ${formatCount(9950)}`)
    expect(w.get('[data-testid="bench-row-bench-1m"]').text()).toContain('not seeded')
  })

  it('draws three empty rows before anything is seeded', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    const w = mountBench()
    await flushPromises()
    expect(w.findAll('[data-testid^="bench-row-"]')).toHaveLength(3)
  })
})

describe('it writes only when told to', () => {
  it('reads on mount but seeds nothing', async () => {
    const read = vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    const seed = vi.spyOn(api, 'seedFixture')
    mountBench()
    await flushPromises()
    expect(read).toHaveBeenCalled()
    expect(seed).not.toHaveBeenCalled()
  })

  it('prints a terminal command for every size it seeds', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    const w = mountBench()
    await flushPromises()
    const said = w.get('[data-testid="bench-cmd"]').text()
    for (const size of [10000, 100000, 1000000]) {
      expect(said).toContain(`cqrs bench -size ${size}`)
    }
  })

  // Three commands set as one run of mono text read as one long command. A
  // reader who copies the middle of that line pastes something the binary
  // rejects, and the guard in commands.spec.js cannot see a layout mistake.
  it('gives each command a line of its own', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    const w = mountBench()
    await flushPromises()
    const lines = w.findAll('[data-testid="bench-cmd"] li')
    expect(lines.map((l) => l.text())).toEqual([
      'cqrs bench -size 10000',
      'cqrs bench -size 100000',
      'cqrs bench -size 1000000',
    ])
  })

  it('says so when the write side cannot be reached', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue({
      kind: 'broken',
      error: 'Unreachable',
      message: 'is `cqrs serve` running?',
    })
    const w = mountBench()
    await flushPromises()
    expect(w.get('[data-testid="bench-broken"]').text()).toContain('Unreachable')
    expect(w.find('[data-testid="bench-holds"]').exists()).toBe(false)
  })
})

describe('it reports, it does not aim', () => {
  // D16. Aiming the measurement is the picker's job now, and two controls
  // aiming one measurement is the defect 04.7.18 exists to remove.
  it('offers no measure button and emits no target', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(partial)
    const w = mountBench()
    await flushPromises()
    expect(w.findAll('[data-testid^="bench-measure-"]')).toHaveLength(0)
    expect(w.emitted('measure')).toBeUndefined()
  })

  it('hands its state up so the picker can list the fixtures', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(partial)
    const w = mountBench()
    await flushPromises()
    expect(w.emitted('state').at(-1)).toEqual([partial])
  })
})
