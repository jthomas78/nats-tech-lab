import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import StarvationRuns from './StarvationRuns.vue'
import { STARVATION_CAPS } from '../view/lessons.js'
import * as api from '../pool/api.js'

// Lesson 02 · Starvation, run live (plan 04.9.6, decisions D5, D7, D12).
//
// This tab used to print three numbers measured on 2026-09-16 and nothing
// else. The claim it makes -- that MaxAckPending is a loss dial and not a
// starvation dial -- is the kind a reader should be able to disbelieve and
// then check. So the numbers now come from four runs made on the press.
//
// D12: four caps, 1 / 3 / 8 / 64. The list is not shortened. A cap of 1 is the
// clearest possible starvation setup, a cap of 64 is past the point where the
// cap binds with eight workers, and two points are not a curve.
//
// D5: a row that has not been run is greyed, never filled. There is no
// recorded fallback, because a fallback shows numbers after a run that failed.

const ok = (over = {}) => ({
  kind: 'ok', workers: 8, maxPending: 1, ackWait: '30s', killAt: 0,
  events: 10_000, acked: 10_000, dropped: 0, seconds: 10, rate: 1000,
  share: { workers: 8, busy: 8, idle: 0, acked: [] },
  ...over,
})

const at = (w, id) => w.find(`[data-testid="${id}"]`)
const rowsOf = (w) => w.findAll('[data-testid^="starvation-cap-"]')

// Eight heartbeats, shaped the way KV odometer-pool-workers holds them.
const workerRows = (acked, count = 8) =>
  new Map(Array.from({ length: count }, (_, i) => [
    String(i + 1),
    { worker: i + 1, status: 'working', acked: Math.floor(acked / count), dropped: 0 },
  ]))

beforeEach(() => {
  vi.restoreAllMocks()
  vi.spyOn(api, 'seedPool').mockResolvedValue({ kind: 'ok', events: 10_000 })
})

describe('before anybody presses Run', () => {
  it('shows one row per cap, and every one of them is empty', () => {
    const w = mount(StarvationRuns)
    expect(rowsOf(w)).toHaveLength(4)
    expect(rowsOf(w).every((r) => r.classes('empty'))).toBe(true)
  })

  // D12 — the full list, in order, not a shortened one.
  it('runs the caps the decision names, 1 / 3 / 8 / 64', () => {
    expect(STARVATION_CAPS).toEqual([1, 3, 8, 64])
    const w = mount(StarvationRuns)
    expect(rowsOf(w).map((r) => r.attributes('data-testid')))
      .toEqual(STARVATION_CAPS.map((c) => `starvation-cap-${c}`))
  })

  // An empty row still has to say what it WILL do, or the reader cannot price
  // the press from the table.
  it('names each cap on its empty row', () => {
    expect(rowsOf(mount(StarvationRuns))[3].text()).toContain('64')
  })

  it('carries no measured number until a run makes one', () => {
    const w = mount(StarvationRuns)
    for (const r of rowsOf(w)) expect(r.text()).not.toMatch(/\d+\.\d+s/)
  })
})

describe('after a press that got part way', () => {
  // The heart of D5. Two runs done, two not: two rows filled, two still grey.
  it('fills the rows it ran and leaves the rest empty', async () => {
    let n = 0
    vi.spyOn(api, 'runPool').mockImplementation(async (cfg) => {
      n += 1
      return n <= 2
        ? ok({ maxPending: cfg.maxPending, seconds: 10 + n, rate: 1000 - n })
        : { kind: 'broken', error: 'Unreachable', message: 'no answer' }
    })

    const w = mount(StarvationRuns)
    await at(w, 'run-go').trigger('click')
    await flushPromises()

    const rows = rowsOf(w)
    expect(rows[0].classes('empty')).toBe(false)
    expect(rows[1].classes('empty')).toBe(false)
    expect(rows[2].classes('empty')).toBe(true)
    expect(rows[3].classes('empty')).toBe(true)
    expect(at(w, 'starvation-broken').text()).toContain('no answer')
  })
})

describe('a whole set', () => {
  it('re-seeds between runs, so every run folds the same log', async () => {
    vi.spyOn(api, 'runPool').mockResolvedValue(ok())
    const w = mount(StarvationRuns)
    await at(w, 'run-go').trigger('click')
    await flushPromises()
    // Four runs, three gaps.
    expect(api.runPool).toHaveBeenCalledTimes(4)
    expect(api.seedPool).toHaveBeenCalledTimes(3)
  })

  it('asks for each cap in turn, at one worker count', async () => {
    vi.spyOn(api, 'runPool').mockResolvedValue(ok())
    const w = mount(StarvationRuns)
    await at(w, 'run-go').trigger('click')
    await flushPromises()
    const caps = api.runPool.mock.calls.map(([cfg]) => cfg.maxPending)
    expect(caps).toEqual(STARVATION_CAPS)
    expect(api.runPool.mock.calls.every(([cfg]) => cfg.workers === 8)).toBe(true)
    expect(api.runPool.mock.calls.every(([cfg]) => cfg.drain === true)).toBe(true)
  })

  it('shows the real numbers it was given, and prices them against each other', async () => {
    let n = 0
    vi.spyOn(api, 'runPool').mockImplementation(async (cfg) => {
      n += 1
      return ok({ maxPending: cfg.maxPending, seconds: n * 10, rate: 100 * n, dropped: n - 1 })
    })
    const w = mount(StarvationRuns)
    await at(w, 'run-go').trigger('click')
    await flushPromises()
    expect(rowsOf(w)[0].text()).toContain('10')
    expect(rowsOf(w)[3].text()).toContain('40')
  })
})

describe('what the press costs', () => {
  it('prices four runs before the reader commits to them', () => {
    const cost = at(mount(StarvationRuns), 'run-cost').text()
    expect(cost).toContain('4 runs')
    expect(cost).toContain('8 workers')
  })

  // D2 — the four commands are printed, so the table can be reproduced in a
  // terminal without the screen.
  it('prints one command per cap', () => {
    const lines = at(mount(StarvationRuns), 'run-cmd').findAll('code').map((c) => c.text())
    expect(lines).toHaveLength(4)
    expect(lines[0]).toBe('cqrs pool -drain -workers 8 -max-pending 1')
  })
})

describe('the bar, driven by the workers bucket', () => {
  // D3 — the ONLY input to the percentage is the rows the page already
  // watches. Found in the browser, not by a spec: the bar sat at 0% for a
  // whole set because the watch was never handed to the run set at all. A
  // press that produces no progress is the failure the bar exists to prevent.
  it('moves the bar as the workers report', async () => {
    let release
    vi.spyOn(api, 'runPool').mockImplementation(
      () => new Promise((r) => { release = () => r(ok()) }),
    )

    const w = mount(StarvationRuns)
    await at(w, 'run-go').trigger('click')
    await flushPromises()

    // Half the log acked, on the first of four runs -> an eighth of the set.
    await w.setProps({ workers: workerRows(5_000) })
    await flushPromises()
    expect(at(w, 'run-bar').text()).toContain('13')

    release()
    await flushPromises()
  })
})
