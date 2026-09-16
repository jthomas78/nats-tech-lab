import { beforeEach, describe, expect, it } from 'vitest'

import { useRunProgress } from './useRunProgress.js'

// The progress bar for a run set (plan 04.9.4, decisions D3, D11, D12).
//
// D3 is the whole design: there is NO new transport. The pool already writes
// one key per worker into KV odometer-pool-workers, the browser already
// watches that bucket, and this turns those rows into a percentage. A run that
// reported its own progress would need a second channel that only agreed with
// the first when nothing went wrong.
//
// D12 is the condition the phase was approved on: the bar is visible from the
// first press to the last run. Not one bar per run -- a bar that vanished
// between runs would read as a finished set, four times.
//
// Three things have to hold:
//
//   The percentage counts the WHOLE set, not the run in flight. Run 2 of 4
//   half done is 37.5%, not 50%.
//   A run that has stopped moving says so. The pool's own ack-wait is the
//   only honest threshold: below it, a quiet worker is a worker waiting for
//   redelivery, which is the lesson, not a fault.
//   A page reload mid-run does not lose the set (D11). The run is in the
//   shim, not in the tab.

const store = new Map()
const session = {
  getItem: (k) => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => store.set(k, String(v)),
  removeItem: (k) => store.delete(k),
}

// Eight workers that have acked `each` between them, shaped the way KV
// odometer-pool-workers holds them.
const rows = (each, count = 8) =>
  Array.from({ length: count }, (_, i) => ({
    worker: i + 1,
    status: 'working',
    acked: Math.floor(each / count),
    dropped: 0,
  }))

beforeEach(() => store.clear())

describe('a set of runs, one bar', () => {
  it('is not running before the first press', () => {
    const p = useRunProgress({ session })
    expect(p.running.value).toBe(false)
    expect(p.percent.value).toBe(0)
  })

  it('counts the whole set, not the run in flight', () => {
    const p = useRunProgress({ session })
    p.start({ label: 'Starvation', runs: 4, events: 10_000, ackWaitMs: 30_000 })

    // Run 1 of 4, half its events acked -> an eighth of the set.
    p.observe(rows(5_000), 1_000)
    expect(p.runIndex.value).toBe(1)
    expect(p.runsTotal.value).toBe(4)
    expect(p.percent.value).toBe(13)

    // Run 2 of 4, half done -> three eighths.
    p.nextRun()
    p.observe(rows(5_000), 2_000)
    expect(p.runIndex.value).toBe(2)
    expect(p.percent.value).toBe(38)
  })

  it('says which run it is on, in words a reader can check', () => {
    const p = useRunProgress({ session })
    p.start({ label: 'Performance', runs: 4, events: 10_000, ackWaitMs: 30_000 })
    p.nextRun()
    p.nextRun()
    expect(p.note.value).toContain('run 3 of 4')
  })

  // D12. The bar stays up while the log is rebuilt between runs, so the set
  // never looks finished early.
  it('stays visible while the log is re-seeded between runs', () => {
    const p = useRunProgress({ session })
    p.start({ label: 'Starvation', runs: 4, events: 10_000, ackWaitMs: 30_000 })
    p.reseeding()
    expect(p.running.value).toBe(true)
    expect(p.note.value).toContain('re-seeding')
  })

  it('reaches 100 and stops when the last run ends', () => {
    const p = useRunProgress({ session })
    p.start({ label: 'Starvation', runs: 2, events: 10_000, ackWaitMs: 30_000 })
    p.nextRun()
    p.observe(rows(10_000), 5_000)
    p.finish()
    expect(p.percent.value).toBe(100)
    expect(p.running.value).toBe(false)
  })
})

describe('a run that has stopped moving', () => {
  // The threshold is the pool's OWN -ack-wait. A worker quiet for less than
  // that is waiting for a redelivery, which is the lesson rather than a
  // fault; quiet for longer than that and nothing is coming.
  it('is quiet, not stalled, inside the ack-wait', () => {
    const p = useRunProgress({ session })
    p.start({ label: 'Redelivery', runs: 1, events: 10_000, ackWaitMs: 30_000 })
    p.observe(rows(1_000), 1_000)
    p.observe(rows(1_000), 20_000)
    expect(p.stalled.value).toBe(false)
  })

  it('is stalled once nothing has moved for longer than the ack-wait', () => {
    const p = useRunProgress({ session })
    p.start({ label: 'Redelivery', runs: 1, events: 10_000, ackWaitMs: 30_000 })
    p.observe(rows(1_000), 1_000)
    p.observe(rows(1_000), 40_000)
    expect(p.stalled.value).toBe(true)
    expect(p.note.value).toContain('ack-wait')
  })

  // A parameterised threshold is only proved parameterised by two inputs.
  it('takes the threshold from the run, not from a constant', () => {
    const p = useRunProgress({ session })
    p.start({ label: 'Live', runs: 1, events: 10_000, ackWaitMs: 5_000 })
    p.observe(rows(1_000), 1_000)
    p.observe(rows(1_000), 10_000)
    expect(p.stalled.value).toBe(true)
  })

  it('clears the stall as soon as a worker acks again', () => {
    const p = useRunProgress({ session })
    p.start({ label: 'Redelivery', runs: 1, events: 10_000, ackWaitMs: 30_000 })
    p.observe(rows(1_000), 1_000)
    p.observe(rows(1_000), 40_000)
    p.observe(rows(2_000), 41_000)
    expect(p.stalled.value).toBe(false)
  })
})

describe('a page reload in the middle of a set', () => {
  // D11. The run lives in the shim, not in the tab. A reader who reloads must
  // find the set still going, not a screen that has forgotten it.
  it('picks the set back up where it was', () => {
    const before = useRunProgress({ session })
    before.start({ label: 'Starvation', runs: 4, events: 10_000, ackWaitMs: 30_000 })
    before.nextRun()
    before.nextRun()

    // A reload: everything in memory is gone, sessionStorage is not.
    const after = useRunProgress({ session })
    expect(after.running.value).toBe(true)
    expect(after.runIndex.value).toBe(3)
    expect(after.runsTotal.value).toBe(4)
    expect(after.note.value).toContain('Starvation')
  })

  it('does not resurrect a set that finished', () => {
    const before = useRunProgress({ session })
    before.start({ label: 'Starvation', runs: 1, events: 10_000, ackWaitMs: 30_000 })
    before.finish()

    const after = useRunProgress({ session })
    expect(after.running.value).toBe(false)
  })
})

describe('the bar never runs backwards', () => {
  // Found in the browser: run 1 finished at 12%, then the re-seed put the bar
  // back to 0%. A reader watching that learns the screen is lying to them, not
  // that a log is being rebuilt. A run that ENDED is a run that is DONE, so
  // the re-seed holds the set at the boundary it reached.
  it('holds the finished run at its boundary while re-seeding', () => {
    const p = useRunProgress({ session })
    p.start({ label: 'Starvation', runs: 4, events: 10_000, ackWaitMs: 30_000 })

    p.observe(rows(5_000))
    expect(p.percent.value).toBe(13)

    p.reseeding()
    expect(p.percent.value).toBe(25)

    p.nextRun()
    expect(p.percent.value).toBe(25)
  })
})
