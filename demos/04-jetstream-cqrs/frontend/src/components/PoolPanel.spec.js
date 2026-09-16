// PoolPanel is lesson 02's whole screen, so what it must NOT do is as much of
// a rule as what it must. Two of these its exist only to stop a later change
// putting an unmeasured number on the page.

import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  REDELIVERY_MEASURED_AT,
  REDELIVERY_SOURCE,
  redeliveryRows,
} from '../view/redelivery.js'
import {
  LIVE_PLAN,
  PERFORMANCE_WORKERS,
  REDELIVERY_PLAN,
  STARVATION_CAPS,
} from '../view/lessons.js'
import { formatCount } from '../view/format.js'
import PoolPanel from './PoolPanel.vue'

// PoolFixture reads /pool on mount (04.9.3), so mounting the panel now
// touches the network. Stubbed, not left to fail quietly: a unit suite that
// waits on a socket is a suite that is slow for a reason nobody can see.
beforeEach(() => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => ({ stream: 'ODOMETER_POOL', exists: false, events: 0, bytes: 0 }),
  })
})

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

const pool = () => new Map([['pool-01', { id: 'pool-01', totalKm: 1200, lastSeq: 90 }]])
// odometer-pool-truth: the SAME log the pool folds, folded correctly. Not
// odometer-read -- that folds ODOMETER, which lesson 02 no longer touches
// (04.8.8, D3).
const truth = () => new Map([['pool-01', { id: 'pool-01', totalKm: 1478, lastSeq: 94 }]])

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

  // The page heading now says which lesson this is. An eyebrow line above the
  // tabs repeating it was the same words twice, one above the other. The tags
  // beside it were live state, not a label, so they stay.
  it('carries no eyebrow line above the tabs', () => {
    const w = mountPanel()
    expect(w.get('[data-testid="pool-panel"] > header').find('.eyebrow').exists()).toBe(false)
    expect(w.text()).not.toContain('one consumer, many workers')
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
    const w = mountPanel({ head: 94, workers: workers(), pool: pool(), truth: truth() })
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

  it('shows the fold damage against the correct fold of the same log', () => {
    const w = mountPanel({ head: 94, workers: workers(), pool: pool(), truth: truth() })
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

// 04.7.6 inverted this tab. It used to promise starvation, because the
// nats.io worker-pool page says a low cap "starves a large set of workers".
// Four `-drain` runs at 8 workers said otherwise: at every cap, including 1,
// all eight acked, within 2% of each other. The doc is describing an INSTANT
// (5 of 8 parked, which `num_waiting` confirms); the tab was reading it as a
// RUN. These its exist to stop the old claim coming back.
//
// 04.9.6 then took the recorded table out. The claim is surprising enough
// that a reader should be able to disbelieve it and check, so the four runs
// are made on the press. The recorded card and its terminal block are gone
// with it -- a recorded fallback is how a screen shows numbers after a run
// that never happened (D5).
describe('PoolPanel — starvation, run live', () => {
  const starvation = async () => {
    const w = mountPanel({ head: 94, workers: workers() })
    w.vm.tab = 'starvation'
    await w.vm.$nextTick()
    return w
  }

  it('offers the run table whether or not a pool is running', async () => {
    const w = await starvation()
    expect(w.find('[data-testid="starvation-runs"]').exists()).toBe(true)

    const idle = mountPanel()
    idle.vm.tab = 'starvation'
    await idle.vm.$nextTick()
    expect(idle.find('[data-testid="starvation-runs"]').exists()).toBe(true)
  })

  it('never promises that workers will sit and do nothing', async () => {
    const w = await starvation()
    expect(w.text()).not.toContain('do nothing')
    expect(w.text()).not.toContain('watch five')
  })

  it('draws an empty row per cap before anything is run', async () => {
    const w = await starvation()
    for (const cap of STARVATION_CAPS) {
      const row = w.find(`[data-testid="starvation-cap-${cap}"]`)
      expect(row.exists()).toBe(true)
      expect(row.classes('empty')).toBe(true)
    }
  })

  // D5. The recorded table is DELETED, not kept behind the live one.
  it('keeps no recorded table to fall back on', async () => {
    const w = await starvation()
    expect(w.find('[data-testid="starvation-measured"]').exists()).toBe(false)
    expect(w.find('[data-testid="starvation-term"]').exists()).toBe(false)
  })

  it('names the cap as the loss dial, which is the actual finding', async () => {
    const w = await starvation()
    expect(w.find('[data-testid="starvation-runs"]').text()).toContain('loss')
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

  it('does not claim to have timed the live wait', async () => {
    const killed = workers()
    killed.set('worker.03', { id: 'worker.03', worker: 3, status: 'killed', holding: 94, acked: 12, dropped: 0 })
    const w = mountPanel({ head: 94, workers: killed })
    w.vm.tab = 'redelivery'
    await w.vm.$nextTick()
    expect(w.find('[data-testid="redelivery"]').text()).toContain('cannot time that wait')
  })
})

// The measurement, drawn (plan task 04.7.5). These its exist so the figures on
// screen stay tied to view/redelivery.js — if somebody edits the markup and a
// number drifts from the recorded runs, these fail.
describe('PoolPanel — redelivery measured', () => {
  const open = async () => {
    const w = mountPanel({ head: 94, workers: workers() })
    w.vm.tab = 'redelivery'
    await w.vm.$nextTick()
    return w
  }

  it('shows the recorded runs whether or not a pool is running', async () => {
    const w = await open()
    const card = w.find('[data-testid="redelivery-measured"]')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain(REDELIVERY_MEASURED_AT)
  })

  it('draws one row per recorded run, with that runs own figures', async () => {
    const w = await open()
    for (const r of redeliveryRows()) {
      const row = w.find(`[data-testid="redelivery-${r.ackWait}"]`)
      expect(row.exists()).toBe(true)
      expect(row.text()).toContain(String(r.waitedSeconds))
      expect(row.text()).toContain(r.handoff)
      expect(row.text()).toContain('dropped')
    }
  })

  it('says the redelivery went to a different worker', async () => {
    const w = await open()
    expect(w.find('[data-testid="redelivery-measured"]').text()).toContain('different worker')
  })

  it('says the wait recovered nothing, which is the finding', async () => {
    const w = await open()
    const text = w.find('[data-testid="redelivery-measured"]').text()
    expect(text).toContain('dropped on arrival')
    expect(text).toContain('not a recovery')
  })

  it('prints the commands that produced the runs', async () => {
    const w = await open()
    const term = w.find('[data-testid="redelivery-term"]')
    for (const line of REDELIVERY_SOURCE) expect(term.text()).toContain(line)
  })
})

// Performance, run live (04.9.7). This tab held the panel's ONE exception to
// "everything here was watched live" — four rows measured on 2026-09-15 under
// a heading that said "1 vs 4" about its own four rows. The exception is gone.
//
// These its guard the absence. A recorded number cannot creep back as a
// fallback without failing here.
describe('PoolPanel — Performance, run live', () => {
  const scaling = async (props = {}) => {
    const w = mountPanel(props)
    w.vm.tab = 'scaling'
    await w.vm.$nextTick()
    return w
  }

  it('offers the run table instead of a recorded one', async () => {
    const w = await scaling()
    expect(w.find('[data-testid="performance-runs"]').exists()).toBe(true)
  })

  it('has one empty row per worker count, and no bar on any of them', async () => {
    const w = await scaling()
    const rows = w.findAll('[data-testid^="performance-workers-"]')
    expect(rows.map((r) => r.attributes('data-testid')))
      .toEqual(PERFORMANCE_WORKERS.map((n) => `performance-workers-${n}`))
    expect(rows.every((r) => r.classes('empty'))).toBe(true)
  })

  // The recorded cards and the terminal that produced them are GONE, not
  // hidden. A fallback is how a screen shows numbers after a run that never
  // happened.
  it('no longer draws the recorded cards or their terminal', async () => {
    const w = await scaling()
    for (const id of ['scaling-measured', 'pool-bought', 'pool-cost', 'drain-term', 'drain-4']) {
      expect(w.find(`[data-testid="${id}"]`).exists()).toBe(false)
    }
  })

  it('names the one-worker run as the control', async () => {
    const w = await scaling()
    expect(w.find('[data-testid="performance-runs"]').text()).toContain('control')
  })
})
describe('PoolPanel — odometer-pool', () => {
  // Beside the CORRECT fold of the SAME log, not beside the read model. The
  // read model folds ODOMETER, so a row-by-row comparison with it would be
  // two different questions side by side (04.8.8, D3).
  it('lists the pool bucket beside the correct fold of the same log', async () => {
    const w = mountPanel({ head: 94, workers: workers(), pool: pool(), truth: truth() })
    w.vm.tab = 'pool'
    await w.vm.$nextTick()
    const text = w.find('[data-testid="pool-buckets"]').text()
    expect(text).toContain('odometer-pool')
    expect(text).toContain('odometer-pool-truth')
    expect(text).not.toContain('odometer-read')
  })

  it('prints nats kv ls for the bucket it is showing', async () => {
    const w = mountPanel()
    w.vm.tab = 'pool'
    await w.vm.$nextTick()
    expect(w.find('.cmd').text()).toBe('nats kv ls odometer-pool')
  })
})

// A caught-up pool draws `waiting · 0 acked` on every card, which is the same
// picture a broken screen would draw. These its keep the panel saying which one
// it is, and keep the way out of it on the page rather than in the chat log.
describe('PoolPanel — caught up', () => {
  const caught = () => new Map([['truck-7', { id: 'truck-7', totalKm: 1478, lastSeq: 94 }]])

  it('explains an idle pool and prints the command that feeds it', () => {
    const w = mountPanel({ head: 94, workers: workers(), pool: caught(), truth: truth() })
    const hint = w.find('[data-testid="pool-caught-up"]')
    expect(hint.exists()).toBe(true)
    expect(hint.text()).toContain('cqrs pool -seed')
  })

  it('says the same thing where the bars are, because zero bars read as broken too', () => {
    const w = mountPanel({ head: 94, workers: workers(), pool: caught(), truth: truth() })
    expect(w.find('[data-testid="starvation-caught-up"]').exists()).toBe(true)
  })

  it('stays quiet while the fold is still behind the head — there IS work', () => {
    const w = mountPanel({ head: 94, workers: workers(), pool: pool(), truth: truth() })
    expect(w.find('[data-testid="pool-caught-up"]').exists()).toBe(false)
    expect(w.find('[data-testid="starvation-caught-up"]').exists()).toBe(false)
  })

  it('stays quiet when no pool is running at all', () => {
    expect(mountPanel().find('[data-testid="pool-caught-up"]').exists()).toBe(false)
  })
})

// 04.8.9 — lesson 02 says which log it is reading.
//
// The screen used to name ODOMETER in its prose and print lesson 01's seed
// command as the way out of a caught-up pool. Neither was true after 04.8.6.
// The failure mode is quiet: every word still reads as plausible, and the
// reader is simply told the wrong thing about the thing in front of them.
//
// ODOMETER is a PREFIX of ODOMETER_POOL, so the check has to be a boundary
// match. A plain `not.toContain('ODOMETER')` would fail on the correct name.
const BARE_ODOMETER = /ODOMETER(?!_POOL)/

describe('lesson 02 names its own log and nothing else', () => {
  const everyTab = async (props) => {
    const w = mountPanel(props)
    const seen = []
    for (const key of ['live', 'starvation', 'redelivery', 'scaling', 'pool']) {
      w.vm.tab = key
      await w.vm.$nextTick()
      seen.push([key, w.text()])
    }
    return seen
  }

  it('never names the demo own log, on any tab', async () => {
    const props = { head: 94, messages: 10_000, bytes: 810_000, workers: workers(), pool: pool(), truth: truth() }
    for (const [key, text] of await everyTab(props)) {
      expect(text, `tab ${key} still names ODOMETER`).not.toMatch(BARE_ODOMETER)
    }
  })

  it('names the pool log where it names a log at all', async () => {
    const props = { head: 94, messages: 10_000, bytes: 810_000, workers: workers(), pool: pool(), truth: truth() }
    const [[, live]] = await everyTab(props)
    expect(live).toContain('ODOMETER_POOL')
  })

  // The standing rule, set by the user 2026-09-16. This screen reports its
  // log's length, so it reports the bytes as well.
  it('prices its log — a count never appears alone', () => {
    const w = mountPanel({ head: 94, messages: 10_000, bytes: 810_000, workers: workers(), pool: pool() })
    const size = w.get('[data-testid="pool-log-size"]').text()
    expect(size).toContain(formatCount(10_000))
    expect(size).toMatch(/KiB|MiB|bytes/)
  })

  // The count is LIVE. The panel used to print "74 109 events" as prose in its
  // own header, which contradicted view/drain.js's recorded 10 029 on the next
  // tab over. It now follows the wire.
  //
  // The recorded-run footnotes elsewhere on this screen still name 74 109, and
  // they must: those are the provenance of numbers that were actually measured
  // on a log of that size. A live count and a recorded one are different
  // claims, so only the live one is checked here.
  it('reads the count off the wire, not out of the prose', async () => {
    const w = mountPanel({ head: 94, messages: 10_000, bytes: 810_000, workers: workers(), pool: pool() })
    const size = () => w.get('[data-testid="pool-log-size"]').text()
    expect(size()).toContain(formatCount(10_000))
    await w.setProps({ messages: 25_000, bytes: 2_000_000 })
    expect(size()).toContain(formatCount(25_000))
    expect(size()).not.toContain(formatCount(10_000))
  })

  // A pool with nothing to fold is told how to get events into ITS log.
  // `cqrs seed` fills ODOMETER, which would have left the pool just as idle
  // and buried lesson 01's log at the same time.
  it('offers the pool own seed when the pool has caught up', async () => {
    const caught = new Map([['pool-01', { id: 'pool-01', totalKm: 1478, lastSeq: 94 }]])
    const w = mountPanel({ head: 94, messages: 94, bytes: 8000, workers: workers(), pool: caught, truth: truth() })
    const hint = w.get('[data-testid="pool-caught-up"]').text()
    expect(hint).toContain('cqrs pool -seed')
    expect(hint).not.toContain('cqrs seed ')
  })
})

// Run buttons on the two single-run tabs (04.9.8, D6, D8).
//
// Every TabPanel renders, hidden or not, so "which tab am I on" cannot tell
// these two runs apart. They are told apart by the PLAN they carry, which is
// the thing that actually differs — and which the tab header prints.
describe('PoolPanel — Live and Redelivery run themselves', () => {
  const singles = () =>
    mountPanel().findAllComponents({ name: 'SingleRun' })

  const withPlan = (plan) =>
    singles().find((c) => c.props('plan') === plan)

  it('gives Live and Redelivery a run of their own, and nothing else', () => {
    const plans = singles().map((c) => c.props('plan'))
    expect(plans).toHaveLength(2)
    expect(plans).toContain(LIVE_PLAN)
    expect(plans).toContain(REDELIVERY_PLAN)
  })

  it('offers a Run on each of them', () => {
    for (const plan of [LIVE_PLAN, REDELIVERY_PLAN]) {
      const one = withPlan(plan)
      expect(one).toBeTruthy()
      expect(one.find('[data-testid="run-go"]').exists()).toBe(true)
    }
  })

  // D6 — Live runs open-ended, so it is the only run that can be stopped.
  // Redelivery ends by itself and must not suggest otherwise. The claim is
  // about the PROP, because the control only draws Stop once a run is under
  // way: a spec that read the button would pass on a run that would grow one.
  it('gives the Stop control to Live and to nothing else', () => {
    expect(withPlan(LIVE_PLAN).props('stoppable')).toBe(true)
    expect(withPlan(REDELIVERY_PLAN).props('stoppable')).toBe(false)
    // The multi-run tabs have no SingleRun at all, which is the strongest
    // form of "cannot be stopped".
    expect(singles()).toHaveLength(2)
  })

  // D8 — the screen refuses a second pool instead of warning about one, so
  // the warning goes. The shim knows; the prose only guessed.
  it('no longer tells the reader to stop a pool by hand', () => {
    expect(mountPanel().text()).not.toContain('Stop any pool you already have running')
  })
})

// What locks a Run button (04.9.8, D8). Found by clicking.
//
// The panel used to lock on `health.running`, which is "the workers bucket
// has rows in it". Those rows OUTLIVE the run — a worker writes a last
// heartbeat and stops, the key stays. So the first run of the day greyed
// every Run button on lesson 02 for good.
//
// The shim is the authority: it holds the lock, it refuses the second run,
// and it says so in GET /pool. That is what the buttons must read.
describe('PoolPanel — what greys a Run button', () => {
  const lockedFlags = (w) =>
    ['SingleRun', 'StarvationRuns', 'PerformanceRuns'].flatMap((name) =>
      w.findAllComponents({ name }).map((c) => c.props('locked')),
    )

  const withShim = async (running) => {
    const w = mountPanel({ workers: workers() })
    w.findComponent({ name: 'PoolFixture' }).vm.$emit('state', {
      kind: 'ok', running, runningWorkers: running ? 4 : 0,
    })
    await w.vm.$nextTick()
    return w
  }

  it('greys every run while the shim holds one', async () => {
    expect(lockedFlags(await withShim(true))).not.toContain(false)
  })

  // The stale rows are still there. They must not lock anything.
  it('frees them again when the shim is idle, worker rows and all', async () => {
    expect(lockedFlags(await withShim(false))).not.toContain(true)
  })
})
