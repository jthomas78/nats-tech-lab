import PrimeVue from 'primevue/config'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import BenchFixture from './BenchFixture.vue'
import * as api from '../rehydrate/api.js'
import { formatCount } from '../view/format.js'

// The acceptance test for the seed control in task 04.7.16.
//
// Four things must hold:
//
//   1. A length is never shown on its own. The standing rule set by the user
//      2026-09-16 — every message count is shown with the bytes it consumes.
//      A spec that only checked the count would let the bytes quietly vanish.
//   2. The sizes come from the server, not from this file. The screen must
//      never offer a size the write side would refuse.
//   3. Nothing is seeded until a human presses Seed. Reading is a GET and may
//      run on mount; writing a million events may not.
//   4. An empty fixture says so plainly, and still prints its bytes.

const mountBench = () => mount(BenchFixture, { global: { plugins: [PrimeVue] } })

const seeded = {
  kind: 'ok',
  stream: 'ODOMETER_BENCH',
  subject: 'evt.odometer-bench.>',
  writeKv: 'odometer-bench-write',
  exists: true,
  events: 1_000_000,
  bytes: 83_000_000,
  sizes: [10000, 100000, 1000000],
  fixtures: [
    { size: 1000000, vehicle: 'bench-1m', events: 1000000, snapSeq: 999950, tailLeft: 50 },
  ],
  elapsedMs: 2195,
}

const empty = { ...seeded, exists: false, events: 0, bytes: 0, fixtures: [] }

beforeEach(() => vi.restoreAllMocks())

describe('a length never travels alone', () => {
  it('shows the bytes beside the event count', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(seeded)
    const w = mountBench()
    await flushPromises()
    const said = w.find('[data-testid="bench-holds"]').text()
    // formatCount groups with a non-breaking thin space, not a plain one.
    // Spelling it by hand here would pass for the wrong reason.
    expect(said).toContain(formatCount(1_000_000))
    expect(said).toContain('79.2 MiB')
  })

  it('shows the bytes even when nothing is seeded', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    const w = mountBench()
    await flushPromises()
    const said = w.find('[data-testid="bench-empty"]').text()
    expect(said).toContain('0 events')
    expect(said).toContain('0 B')
    expect(w.find('[data-testid="bench-fixtures"]').exists()).toBe(false)
  })
})

describe('the server owns the sizes', () => {
  it('offers exactly what the write side reports', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue({ ...empty, sizes: [500, 5000] })
    const w = mountBench()
    await flushPromises()
    expect(w.find('[data-testid="bench-size-500"]').exists()).toBe(true)
    expect(w.find('[data-testid="bench-size-5000"]').exists()).toBe(true)
    expect(w.find('[data-testid="bench-size-1000000"]').exists()).toBe(false)
  })

  it('seeds the size that was picked', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    const seed = vi.spyOn(api, 'seedFixture').mockResolvedValue(seeded)
    const w = mountBench()
    await flushPromises()
    await w.find('[data-testid="bench-size-100000"]').trigger('click')
    await w.find('[data-testid="bench-seed"]').trigger('click')
    await flushPromises()
    expect(seed).toHaveBeenCalledWith(100000)
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

  it('prints the terminal command for the picked size', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(empty)
    const w = mountBench()
    await flushPromises()
    await w.find('[data-testid="bench-size-1000000"]').trigger('click')
    expect(w.find('[data-testid="bench-cmd"]').text()).toBe('cqrs bench -size 1000000')
  })
})

describe('it hands a fixture to the measurement', () => {
  it('names the bench source, not just the vehicle', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue(seeded)
    const w = mountBench()
    await flushPromises()
    await w.find('[data-testid="bench-measure-bench-1m"]').trigger('click')
    expect(w.emitted('measure')[0]).toEqual([{ source: 'bench', vehicle: 'bench-1m' }])
  })

  it('says so when the write side cannot be reached', async () => {
    vi.spyOn(api, 'fetchBench').mockResolvedValue({
      kind: 'broken',
      error: 'Unreachable',
      message: 'is `cqrs serve` running?',
    })
    const w = mountBench()
    await flushPromises()
    expect(w.find('[data-testid="bench-broken"]').text()).toContain('Unreachable')
    expect(w.find('[data-testid="bench-holds"]').exists()).toBe(false)
  })
})
