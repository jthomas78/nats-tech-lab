import PrimeVue from 'primevue/config'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import PoolFixture from './PoolFixture.vue'
import PoolPanel from './PoolPanel.vue'
import * as api from '../pool/api.js'
import { POOL_POLL_MS, POOL_RM_CMD, POOL_SEED_CMD } from '../view/lessons.js'
import { formatCount } from '../view/format.js'

// The acceptance test for the pool's seed group, task 04.9.3 (D1, D2).
//
// Same shape as BenchFixture: one group, one job — say what is in
// ODOMETER_POOL, and put it there. Four things must hold:
//
//   1. The stream is named ONCE, with its count and its bytes. The standing
//      rule from 2026-09-16: a length never travels alone.
//   2. ONE primary button seeds it.
//   3. The button PRINTS ITS COMMANDS. Every number this demo shows must be
//      reproducible in a terminal.
//   4. It sits ABOVE the tab strip, not inside a tab. The log is the same log
//      on all four tabs, so a seed control inside one of them would read as
//      belonging to that tab's measurement (D1).

// messages/bytes come from the live KV+stream watch, the same wire the rest of
// the panel reads. The fixture's own GET says whether a run holds the shim and
// which sizes it will take; it does NOT get to state the length. A snapshot is
// always stale, and a stale length sitting beside a live one is the defect
// 04.8.9 already removed once.
const mountFixture = (props = { messages: 10_000, bytes: 810_120 }) =>
  mount(PoolFixture, { props, global: { plugins: [PrimeVue] } })

const seeded = {
  kind: 'ok',
  stream: 'ODOMETER_POOL',
  subject: 'evt.odometer-pool.>',
  truthKv: 'odometer-pool-truth',
  exists: true,
  events: 10_000,
  bytes: 810_120,
  sizes: [10000, 100000, 1000000],
  vehicles: ['pool-01', 'pool-02'],
  running: false,
  runningWorkers: 0,
  runningNote: '',
}

const empty = { ...seeded, exists: false, events: 0, bytes: 0 }

const times = (w, needle) => w.text().split(needle).length - 1

beforeEach(() => vi.restoreAllMocks())

describe('the header is the whole report', () => {
  it('names the log, its count and its bytes exactly once', async () => {
    vi.spyOn(api, 'fetchPool').mockResolvedValue(seeded)
    const w = mountFixture()
    await flushPromises()

    const head = w.get('[data-testid="pool-fixture-holds"]').text()
    expect(head).toContain('ODOMETER_POOL')
    expect(head).toContain(formatCount(10_000))
    expect(head).toContain('791.1 KiB')

    expect(times(w, 'ODOMETER_POOL')).toBe(1)
  })

  // ODOMETER is a PREFIX of ODOMETER_POOL, so a half-copied name still reads
  // as plausible. The boundary is the whole check.
  it('never names the demo\'s own log', async () => {
    vi.spyOn(api, 'fetchPool').mockResolvedValue(seeded)
    const w = mountFixture()
    await flushPromises()
    expect(w.text()).not.toMatch(/ODOMETER(?!_POOL)/)
  })

  it('says an empty log is empty rather than showing nothing', async () => {
    vi.spyOn(api, 'fetchPool').mockResolvedValue(empty)
    const w = mountFixture({ messages: 0, bytes: 0 })
    await flushPromises()
    expect(w.get('[data-testid="pool-fixture-holds"]').text()).toContain('nothing yet')
  })
})

describe('the length follows the wire', () => {
  // The GET is a snapshot and a snapshot is always stale. The KV and stream
  // watches are not, so the headline length comes from them -- and changes
  // without anybody pressing anything.
  it('re-prices itself when the log grows under it', async () => {
    vi.spyOn(api, 'fetchPool').mockResolvedValue(seeded)
    const w = mountFixture({ messages: 10_000, bytes: 810_120 })
    await flushPromises()
    expect(w.get('[data-testid="pool-log-size"]').text()).toContain(formatCount(10_000))

    await w.setProps({ messages: 25_000, bytes: 2_000_000 })
    const size = w.get('[data-testid="pool-log-size"]').text()
    expect(size).toContain(formatCount(25_000))
    expect(size).not.toContain(formatCount(10_000))
  })
})

// Found in the browser, not by a spec: the log was deleted, the server
// agreed it was gone, and the header still read 10 000 events.
//
// Neither source can see the whole truth. The stream watch sees an APPEND and
// never a deletion -- a stream that has gone simply stops sending. The GET
// sees the log as it was when it was asked and never moves after. So the
// later of the two wins: a press refreshes from the GET, an event refreshes
// from the wire.
describe('a deleted log stops being reported', () => {
  it('drops to empty after a delete, though the wire never said so', async () => {
    vi.spyOn(api, 'fetchPool').mockResolvedValue(seeded)
    vi.spyOn(api, 'dropPool').mockResolvedValue(empty)
    const w = mountFixture({ messages: 10_000, bytes: 810_120 })
    await flushPromises()
    expect(w.get('[data-testid="pool-log-size"]').text()).toContain(formatCount(10_000))

    // The wire is left exactly as it was. A deletion sends nothing.
    await w.get('[data-testid="pool-fixture-rm"]').trigger('click')
    await flushPromises()

    expect(w.get('[data-testid="pool-fixture-holds"]').text()).toContain('nothing yet')
  })

  it('fills again after a seed, though the wire never said so either', async () => {
    vi.spyOn(api, 'fetchPool').mockResolvedValue(empty)
    vi.spyOn(api, 'seedPool').mockResolvedValue(seeded)
    const w = mountFixture({ messages: 0, bytes: 0 })
    await flushPromises()
    expect(w.get('[data-testid="pool-fixture-holds"]').text()).toContain('nothing yet')

    await w.get('[data-testid="pool-fixture-rm"]').exists()
    await w.get('[data-testid="pool-fixture-seed"]').trigger('click')
    await flushPromises()

    expect(w.get('[data-testid="pool-log-size"]').text()).toContain(formatCount(10_000))
  })
})

describe('one button, and it prints what it does', () => {
  it('seeds through the shim, not through a described command', async () => {
    vi.spyOn(api, 'fetchPool').mockResolvedValue(empty)
    const seed = vi.spyOn(api, 'seedPool').mockResolvedValue(seeded)
    const w = mountFixture({ messages: 0, bytes: 0 })
    await flushPromises()

    await w.get('[data-testid="pool-fixture-seed"]').trigger('click')
    await flushPromises()

    expect(seed).toHaveBeenCalledWith(10000)
  })

  // D2. A button whose commands are not on screen is a number the reader has
  // to take on trust.
  it('prints the commands it runs', async () => {
    vi.spyOn(api, 'fetchPool').mockResolvedValue(seeded)
    const w = mountFixture()
    await flushPromises()
    const cmds = w.get('[data-testid="pool-fixture-cmd"]').text()
    expect(cmds).toContain(POOL_SEED_CMD)
    expect(cmds).toContain(POOL_RM_CMD)
  })

  it('states the cost before the press, in events and in bytes', async () => {
    vi.spyOn(api, 'fetchPool').mockResolvedValue(empty)
    const w = mountFixture({ messages: 0, bytes: 0 })
    await flushPromises()
    const cost = w.get('[data-testid="pool-fixture-cost"]').text()
    expect(cost).toContain(formatCount(10_000))
    expect(cost).toMatch(/KiB|MiB/)
  })

  // D8. The shim refuses a second pool; the screen must not offer one.
  it('locks the button while a run holds the shim', async () => {
    vi.spyOn(api, 'fetchPool').mockResolvedValue({ ...seeded, running: true, runningWorkers: 8 })
    const w = mountFixture()
    await flushPromises()
    expect(w.get('[data-testid="pool-fixture-seed"]').attributes('disabled')).toBeDefined()
  })
})

describe('where it sits', () => {
  // D1. The log is the same log on all four tabs. A seed control inside one
  // of them would read as belonging to that tab's measurement.
  it('renders above the tab strip, not inside a tab', () => {
    vi.spyOn(api, 'fetchPool').mockResolvedValue(seeded)
    const w = mount(PoolPanel, { global: { plugins: [PrimeVue] } })
    const html = w.html()
    const fixtureAt = html.indexOf('data-testid="pool-fixture"')
    const tabsAt = html.indexOf('data-testid="lesson-02-tab-live"')
    expect(fixtureAt).toBeGreaterThan(-1)
    expect(tabsAt).toBeGreaterThan(-1)
    expect(fixtureAt).toBeLessThan(tabsAt)
  })
})

// Found by clicking, not by a spec (04.9.8).
//
// The fixture read the shim ONCE, on mount. A run that started after that —
// from another tab, or from a terminal — never reached the screen, and a run
// that ENDED left the page thinking the shim was still held. Every Run button
// on lesson 02 then stayed grey until the reader reloaded.
describe('PoolFixture — it keeps asking', () => {
  it('re-reads the shim while the page is open', async () => {
    vi.useFakeTimers()
    const read = vi.spyOn(api, 'fetchPool').mockResolvedValue(seeded)
    const w = mountFixture()
    await flushPromises()
    expect(read).toHaveBeenCalledTimes(1)

    read.mockResolvedValue({ ...seeded, running: true, runningWorkers: 4 })
    await vi.advanceTimersByTimeAsync(POOL_POLL_MS)
    await flushPromises()
    expect(read.mock.calls.length).toBeGreaterThan(1)
    expect(w.emitted('state').at(-1)[0].running).toBe(true)

    // And it stops when the page does. A timer that outlives its component is
    // a leak that only shows up as a test that never finishes.
    w.unmount()
    const after = read.mock.calls.length
    await vi.advanceTimersByTimeAsync(POOL_POLL_MS * 3)
    expect(read.mock.calls.length).toBe(after)
    vi.useRealTimers()
  })
})
