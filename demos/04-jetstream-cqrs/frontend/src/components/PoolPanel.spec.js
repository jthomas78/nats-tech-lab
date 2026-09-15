// PoolPanel is lesson 02's whole screen, so what it must NOT do is as much of
// a rule as what it must. Two of these its exist only to stop a later change
// putting an unmeasured number on the page.

import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import { DRAIN_RUNS, DRAIN_SOURCE } from '../view/drain.js'
import PoolPanel from './PoolPanel.vue'

const mountPanel = (props = {}) =>
  mount(PoolPanel, { props, global: { plugins: [PrimeVue] } })

// Two workers, the way KV odometer-pool-workers holds them: keyed by the raw
// key, shaped by poolWorker().
function workers() {
  return new Map([
    ['worker.01', { id: 'worker.01', worker: 1, status: 'working', holding: 94, acked: 40, dropped: 3 }],
    ['worker.02', { id: 'worker.02', worker: 2, status: 'waiting', holding: 0, acked: 0, dropped: 0 }],
  ])
}

const pool = () => new Map([['truck-7', { id: 'truck-7', totalKm: 1200, lastSeq: 90 }]])
const reads = () => new Map([['truck-7', { id: 'truck-7', totalKm: 1478, lastSeq: 94 }]])

describe('PoolPanel — the tab strip', () => {
  it('offers the four conditions and the bucket the pool folds into', () => {
    const w = mountPanel()
    for (const key of ['live', 'starvation', 'redelivery', 'scaling', 'pool']) {
      expect(w.find(`[data-testid="lesson-02-tab-${key}"]`).exists()).toBe(true)
    }
  })

  it('opens on the Live tab', () => {
    expect(mountPanel().vm.tab).toBe('live')
  })

  it('prints the command that produced the open tab', () => {
    const w = mountPanel()
    expect(w.find('.cmd').text()).toContain('cqrs pool -workers 4')
  })
})

describe('PoolPanel — nothing running', () => {
  it('says the lesson has not been run instead of drawing an empty pool', () => {
    const w = mountPanel()
    expect(w.find('[data-testid="pool-not-running"]').exists()).toBe(true)
    expect(w.find('[data-testid="worker-cards"]').exists()).toBe(false)
  })
})

describe('PoolPanel — the live pool', () => {
  it('draws one card per worker', () => {
    const w = mountPanel({ head: 94, workers: workers(), pool: pool(), reads: reads() })
    expect(w.find('[data-testid="worker-1"]').exists()).toBe(true)
    expect(w.find('[data-testid="worker-2"]').exists()).toBe(true)
  })

  it('says what the working worker is holding', () => {
    const w = mountPanel({ head: 94, workers: workers() })
    expect(w.find('[data-testid="worker-1"]').text()).toContain('#94')
  })

  it('draws the lane', () => {
    const w = mountPanel({ head: 94, workers: workers(), pool: pool() })
    expect(w.find('[data-testid="lag-lane"]').exists()).toBe(true)
  })

  it('shows the fold damage against the read model', () => {
    const w = mountPanel({ head: 94, workers: workers(), pool: pool(), reads: reads() })
    const text = w.find('[data-testid="fold-damage"]').text()
    expect(text).toContain('278 km')
  })

  it('leaves the damage out when there is nothing to compare', () => {
    const w = mountPanel({ head: 94, workers: workers() })
    expect(w.find('[data-testid="fold-damage"]').exists()).toBe(false)
  })
})

describe('PoolPanel — starvation', () => {
  it('draws a bar per worker once the pool runs', async () => {
    const w = mountPanel({ head: 94, workers: workers() })
    w.vm.tab = 'starvation'
    await w.vm.$nextTick()
    const card = w.find('[data-testid="ack-bars"]')
    expect(card.exists()).toBe(true)
    // Scoped to this card on purpose: the 1-vs-4 tab draws bar rows too, and
    // a page-wide count would silently mix the two lessons together.
    expect(card.findAll('.bar-row')).toHaveLength(2)
  })
})

describe('PoolPanel — redelivery', () => {
  it('waits for a killed worker rather than inventing one', async () => {
    const w = mountPanel({ head: 94, workers: workers() })
    w.vm.tab = 'redelivery'
    await w.vm.$nextTick()
    expect(w.find('[data-testid="redelivery-not-running"]').exists()).toBe(true)
  })

  it('names the held sequence when a worker has gone silent', async () => {
    const killed = workers()
    killed.set('worker.03', { id: 'worker.03', worker: 3, status: 'killed', holding: 94, acked: 12, dropped: 0 })
    const w = mountPanel({ head: 94, workers: killed })
    w.vm.tab = 'redelivery'
    await w.vm.$nextTick()
    expect(w.find('[data-testid="redelivery"]').text()).toContain('#94')
  })
})

// The point of the whole file. `-drain` has not been run (plan task 04.7.4),
// so this tab has no numbers, and a later change that fills it in with
// plausible ones must fail here.
// These its exist to keep the screen tied to a real run. The panel's one
// exception to "everything here was watched live" is this tab, so the figures
// it draws must be the recorded ones — if somebody edits the markup and a
// number drifts from view/drain.js, these fail.
describe('PoolPanel — 1 vs 4', () => {
  const scaling = async (props = {}) => {
    const w = mountPanel(props)
    w.vm.tab = 'scaling'
    await w.vm.$nextTick()
    return w
  }

  it('draws one bar per recorded run', async () => {
    const w = await scaling()
    for (const r of DRAIN_RUNS) {
      expect(w.find(`[data-testid="drain-${r.workers}"]`).exists()).toBe(true)
    }
  })

  it('prints the measured drain time on the bar', async () => {
    const w = await scaling()
    expect(w.find('[data-testid="drain-4"]').text()).toContain('6.3 s')
  })

  // The two cards are the lesson. A reader who sees only the first one has
  // been told a half-truth, so both are asserted together.
  it('says what the pool bought', async () => {
    const w = await scaling()
    const text = w.find('[data-testid="pool-bought"]').text()
    expect(text).toContain('1600 events/s')
    expect(text).toContain('3.7x')
    expect(text).toContain('no')
  })

  it('says what it cost, beside what it bought', async () => {
    const w = await scaling()
    const text = w.find('[data-testid="pool-cost"]').text()
    expect(text).toContain('3082')
    expect(text).toContain('31%')
    expect(text).toContain('57%')
  })

  it('shows the one-worker run dropping nothing — the control', async () => {
    const w = await scaling()
    expect(w.find('[data-testid="pool-cost"]').text()).toContain('0')
    expect(w.find('[data-testid="drain-1"]').text()).toContain('23.5 s')
  })

  it('says where and when the runs were made', async () => {
    const w = await scaling()
    expect(w.find('[data-testid="scaling-measured"]').text()).toContain('2026-09-15')
  })

  it('prints every command that produced them', async () => {
    const w = await scaling()
    const text = w.find('[data-testid="drain-term"]').text()
    for (const line of DRAIN_SOURCE) expect(text).toContain(line)
  })
})

// The pool folds into a bucket, so that bucket can be browsed — the same
// read-only view lesson 01 gives odometer-write and odometer-read. Without it
// the damage is one total you have to trust.
describe('PoolPanel — odometer-pool', () => {
  it('lists the pool bucket beside the read model', async () => {
    const w = mountPanel({ head: 94, workers: workers(), pool: pool(), reads: reads() })
    w.vm.tab = 'pool'
    await w.vm.$nextTick()
    const text = w.find('[data-testid="pool-buckets"]').text()
    expect(text).toContain('odometer-pool')
    expect(text).toContain('odometer-read')
  })

  it('prints nats kv ls for the bucket it is showing', async () => {
    const w = mountPanel()
    w.vm.tab = 'pool'
    await w.vm.$nextTick()
    expect(w.find('.cmd').text()).toBe('nats kv ls odometer-pool')
  })
})
