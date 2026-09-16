import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import PerformanceRuns from './PerformanceRuns.vue'
import { PERFORMANCE_WORKERS, tabsFor } from '../view/lessons.js'
import * as api from '../pool/api.js'

// Lesson 02 · Performance, run live (plan 04.9.7, decisions D5, D7, D9, D12).
//
// The tab used to print four rows measured on 2026-09-15 under the heading
// "1 vs 4", which was already a lie about its own contents: it held four
// worker counts, not two. Now it runs them, and the heading says so.
//
// D9 — the other knob. Starvation varies MaxAckPending at a fixed eight
// workers; this varies the workers and leaves the cap alone. One tab per
// knob, so a reader can attribute a change to the thing they turned.

const ok = (over = {}) => ({
  kind: 'ok', workers: 1, maxPending: 1000, ackWait: '30s', killAt: 0,
  events: 10_000, acked: 10_000, dropped: 0, seconds: 20, rate: 500,
  share: { workers: 1, busy: 1, idle: 0, acked: [] },
  ...over,
})

const at = (w, id) => w.find(`[data-testid="${id}"]`)
const rowsOf = (w) => w.findAll('[data-testid^="performance-workers-"]')

const workerRows = (acked, count = 8) =>
  new Map(Array.from({ length: count }, (_, i) => [
    String(i + 1),
    { worker: i + 1, status: 'working', acked: Math.floor(acked / count), dropped: 0 },
  ]))

beforeEach(() => {
  vi.restoreAllMocks()
  vi.spyOn(api, 'seedPool').mockResolvedValue({ kind: 'ok', events: 10_000 })
})

describe('the tab is no longer named after two of its four rows', () => {
  it('has no tab called 1 vs 4 anywhere on lesson 02', () => {
    const labels = tabsFor('lesson-02').map((t) => t.label)
    expect(labels).not.toContain('1 vs 4')
    expect(labels).toContain('Performance')
  })
})

describe('before anybody presses Run', () => {
  it('shows one empty row per worker count', () => {
    const w = mount(PerformanceRuns)
    expect(rowsOf(w)).toHaveLength(4)
    expect(rowsOf(w).every((r) => r.classes('empty'))).toBe(true)
  })

  it('runs the counts the decision names, 1 / 2 / 4 / 8', () => {
    expect(PERFORMANCE_WORKERS).toEqual([1, 2, 4, 8])
    const w = mount(PerformanceRuns)
    expect(rowsOf(w).map((r) => r.attributes('data-testid')))
      .toEqual(PERFORMANCE_WORKERS.map((n) => `performance-workers-${n}`))
  })

  it('carries no measured number until a run makes one', () => {
    for (const r of rowsOf(mount(PerformanceRuns))) expect(r.text()).not.toMatch(/\d+\.\d+s/)
  })

  // The point of the tab is the speed-up, and a speed-up needs two runs. A
  // bar drawn before either is a bar drawn against nothing.
  it('draws no bar on a row that has not run', () => {
    expect(mount(PerformanceRuns).findAll('.fill')).toHaveLength(0)
  })
})

describe('a whole set', () => {
  it('re-seeds between runs, so every run folds the same log', async () => {
    vi.spyOn(api, 'runPool').mockResolvedValue(ok())
    const w = mount(PerformanceRuns)
    await at(w, 'run-go').trigger('click')
    await flushPromises()
    expect(api.runPool).toHaveBeenCalledTimes(4)
    expect(api.seedPool).toHaveBeenCalledTimes(3)
  })

  it('asks for each worker count in turn, at one cap', async () => {
    vi.spyOn(api, 'runPool').mockResolvedValue(ok())
    const w = mount(PerformanceRuns)
    await at(w, 'run-go').trigger('click')
    await flushPromises()
    expect(api.runPool.mock.calls.map(([c]) => c.workers)).toEqual(PERFORMANCE_WORKERS)
    expect(api.runPool.mock.calls.every(([c]) => c.drain === true)).toBe(true)
    const caps = new Set(api.runPool.mock.calls.map(([c]) => c.maxPending))
    expect(caps.size).toBe(1)
  })

  // A bar only means something against a scale, and the scale is the slowest
  // run SO FAR — so the shape appears as the runs land, not at the end.
  it('scales the bars against the slowest run so far', async () => {
    let n = 0
    vi.spyOn(api, 'runPool').mockImplementation(async (cfg) => {
      n += 1
      return ok({ workers: cfg.workers, seconds: 40 / n, rate: 250 * n })
    })
    const w = mount(PerformanceRuns)
    await at(w, 'run-go').trigger('click')
    await flushPromises()

    const widths = w.findAll('.fill').map((f) => f.attributes('style'))
    expect(widths).toHaveLength(4)
    // 40s is the slowest, so the one-worker row is full and the eight-worker
    // row is a quarter of it.
    expect(widths[0]).toContain('100%')
    expect(widths[3]).toContain('25%')
  })

  it('leaves the rows it did not reach empty', async () => {
    let n = 0
    vi.spyOn(api, 'runPool').mockImplementation(async () => {
      n += 1
      return n <= 2 ? ok() : { kind: 'broken', error: 'Unreachable', message: 'no answer' }
    })
    const w = mount(PerformanceRuns)
    await at(w, 'run-go').trigger('click')
    await flushPromises()
    expect(rowsOf(w).map((r) => r.classes('empty'))).toEqual([false, false, true, true])
    expect(at(w, 'performance-broken').text()).toContain('no answer')
  })
})

describe('what the press costs, and what it is equal to', () => {
  it('prices four runs before the reader commits to them', () => {
    expect(at(mount(PerformanceRuns), 'run-cost').text()).toContain('4 runs')
  })

  it('prints one command per worker count', () => {
    const lines = at(mount(PerformanceRuns), 'run-cmd').findAll('code').map((c) => c.text())
    expect(lines).toHaveLength(4)
    expect(lines[0]).toContain('-workers 1')
    expect(lines[3]).toContain('-workers 8')
  })
})

describe('the bar, driven by the workers bucket', () => {
  it('moves the bar as the workers report', async () => {
    let release
    vi.spyOn(api, 'runPool').mockImplementation(
      () => new Promise((r) => { release = () => r(ok()) }),
    )
    const w = mount(PerformanceRuns)
    await at(w, 'run-go').trigger('click')
    await flushPromises()

    await w.setProps({ workers: workerRows(5_000) })
    await flushPromises()
    expect(at(w, 'run-bar').text()).toContain('13')

    release()
    await flushPromises()
  })
})
