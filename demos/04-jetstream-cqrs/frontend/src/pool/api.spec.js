import { describe, expect, it } from 'vitest'

import { describePool, describeRun, dropPool, fetchPool, runPool, seedPool, stopPool } from './api.js'

// The HTTP client for lesson 02's own log (plan 04.9.2, 04.9.3).
//
// Same shape as rehydrate/api.js: it sends, and it hands back an outcome for
// every path including failure. It checks no rule and decides nothing.
//
// Two things it must not do:
//
//   Drop `bytes` when the count is zero. The standing rule says a length never
//   travels alone, and a shape that dropped the field on an empty log would
//   make the rule optional exactly when the reader is deciding whether to
//   spend the disk.
//
//   Seed on a GET. One press writes ten thousand events.

const answer = (status, body) => ({
  ok: status === 200,
  status,
  json: async () => body,
})

const full = {
  stream: 'ODOMETER_POOL',
  subject: 'evt.odometer-pool.>',
  truthKv: 'odometer-pool-truth',
  exists: true,
  events: 10000,
  bytes: 810120,
  sizes: [10000, 100000, 1000000],
  vehicles: ['pool-01', 'pool-02'],
  running: false,
}

describe('reading what the pool log holds', () => {
  it('carries the count and the bytes together', () => {
    const s = describePool({ status: 200, body: full })
    expect(s.kind).toBe('ok')
    expect(s.events).toBe(10000)
    expect(s.bytes).toBe(810120)
  })

  it('still carries bytes when the log is empty', () => {
    const s = describePool({ status: 200, body: { stream: 'ODOMETER_POOL', exists: false } })
    expect(s.exists).toBe(false)
    expect(s.events).toBe(0)
    expect(s.bytes).toBe(0)
  })

  it('reports a run that is already in flight', () => {
    const s = describePool({
      status: 200,
      body: { ...full, running: true, runningWorkers: 8, runningNote: 'a run is already in progress' },
    })
    expect(s.running).toBe(true)
    expect(s.runningWorkers).toBe(8)
    expect(s.runningNote).toContain('already in progress')
  })

  it('turns a refusal into a broken outcome, never an exception', () => {
    const s = describePool({ status: 409, body: { error: 'PoolRunning', message: 'busy' } })
    expect(s.kind).toBe('broken')
    expect(s.error).toBe('PoolRunning')
  })

  it('names the shim when nothing answers at all', async () => {
    const s = await fetchPool({
      fetchImpl: async () => {
        throw new Error('refused')
      },
    })
    expect(s.kind).toBe('broken')
    expect(s.message).toContain('cqrs serve')
  })
})

describe('seeding and dropping', () => {
  it('reads the log with a GET', async () => {
    const seen = []
    await fetchPool({
      fetchImpl: async (url, opts) => {
        seen.push([url, opts.method])
        return answer(200, full)
      },
      base: 'http://x',
    })
    expect(seen).toEqual([['http://x/pool', 'GET']])
  })

  // POST, never GET. Ten thousand events is not something a link preview or a
  // browser prefetch may set off.
  it('seeds with a POST that states the size', async () => {
    const seen = []
    await seedPool(10000, {
      fetchImpl: async (url, opts) => {
        seen.push([url, opts.method, opts.body])
        return answer(200, full)
      },
      base: 'http://x',
    })
    expect(seen[0][0]).toBe('http://x/pool/seed')
    expect(seen[0][1]).toBe('POST')
    expect(JSON.parse(seen[0][2])).toEqual({ size: 10000 })
  })

  it('drops with a POST', async () => {
    const seen = []
    await dropPool({
      fetchImpl: async (url, opts) => {
        seen.push([url, opts.method])
        return answer(200, { ...full, exists: false, events: 0, bytes: 0 })
      },
      base: 'http://x',
    })
    expect(seen).toEqual([['http://x/pool/rm', 'POST']])
  })
})

// ---------------------------------------------------------------------------
// Running a pool (plan 04.9.6).

describe('describeRun', () => {
  const body = {
    workers: 8, maxPending: 1, ackWait: '30s', killAt: 0, drain: true,
    events: 10000, acked: 10000, dropped: 0, elapsedMs: 12500,
    share: { workers: 8, busy: 8, idle: 0, acked: [1250, 1250, 1250, 1250, 1250, 1250, 1250, 1250] },
  }

  it('reports what the run actually did, not what was asked for', () => {
    const r = describeRun({ status: 200, body })
    expect(r.kind).toBe('ok')
    expect(r.events).toBe(10000)
    expect(r.dropped).toBe(0)
    expect(r.seconds).toBeCloseTo(12.5, 3)
  })

  // The rate is the number the tabs compare, so it is computed in ONE place.
  // Two tabs each dividing events by seconds is two chances to divide by zero.
  it('computes the rate from the run, and never divides by zero', () => {
    expect(describeRun({ status: 200, body }).rate).toBe(800)
    expect(describeRun({ status: 200, body: { ...body, elapsedMs: 0 } }).rate).toBe(0)
  })

  it('carries the per-worker share, so a bar chart needs no second call', () => {
    expect(describeRun({ status: 200, body }).share.busy).toBe(8)
    expect(describeRun({ status: 200, body }).share.acked).toHaveLength(8)
  })

  // D8 — the shim allows one run at a time and answers 409. That is not a
  // fault, it is an answer, and the screen says which run is holding it.
  it('tells a second caller who is already running', () => {
    const r = describeRun({
      status: 409,
      body: { error: 'PoolRunning', message: '8 workers since 12:01:00' },
    })
    expect(r.kind).toBe('busy')
    expect(r.message).toContain('8 workers')
  })

  it('turns any other refusal into broken, never an exception', () => {
    expect(describeRun({ status: 400, body: { error: 'BadRequest', message: 'no' } }).kind)
      .toBe('broken')
  })
})

describe('runPool', () => {
  it('posts the config the caller asked for', async () => {
    const calls = []
    const fetchImpl = async (url, init) => {
      calls.push({ url, init })
      return { ok: true, status: 200, json: async () => ({ events: 1, elapsedMs: 1000 }) }
    }
    await runPool({ workers: 8, maxPending: 1, drain: true }, { fetchImpl })
    expect(calls[0].url).toContain('/pool/run')
    expect(calls[0].init.method).toBe('POST')
    expect(JSON.parse(calls[0].init.body)).toEqual({ workers: 8, maxPending: 1, drain: true })
  })
})

// Stopping the run in flight (04.9.8, D6).
describe('stopPool', () => {
  it('posts to the stop route with no body naming a run', async () => {
    const calls = []
    const fetchImpl = async (url, init) => {
      calls.push([url, init])
      return { status: 200, json: async () => ({ stopped: true, message: 'stopped a run of 4 workers' }) }
    }
    const out = await stopPool({ fetchImpl, base: 'http://x' })

    expect(calls[0][0]).toBe('http://x/pool/stop')
    expect(calls[0][1].method).toBe('POST')
    expect(out.kind).toBe('ok')
    expect(out.stopped).toBe(true)
  })

  // Nothing running is an ANSWER, not a fault. A reader who pressed Stop a
  // moment after the run ended by itself got what they asked for.
  it('treats "nothing was running" as a normal answer', async () => {
    const fetchImpl = async () => ({
      status: 200, json: async () => ({ stopped: false, message: 'no pool is running' }),
    })
    const out = await stopPool({ fetchImpl, base: 'http://x' })
    expect(out.kind).toBe('ok')
    expect(out.stopped).toBe(false)
  })

  it('reports a shim that does not answer', async () => {
    const fetchImpl = async () => { throw new Error('connection refused') }
    const out = await stopPool({ fetchImpl, base: 'http://x' })
    expect(out.kind).toBe('broken')
  })
})

// 04.9.9. The Redelivery tab used to draw a row from a file. The run reports
// its own now, and this is where the wire shape becomes the shape the drawing
// reads: seconds instead of milliseconds, and a word instead of a flag.
describe('describeRun — the redelivery the run measured', () => {
  const body = (over = {}) => ({
    workers: 4, maxPending: 1000, ackWait: '30s', killAt: 94,
    events: 10_000, acked: 9_999, dropped: 1, elapsedMs: 31_100,
    share: { workers: 4, busy: 3, idle: 1, acked: [] },
    ...over,
  })

  // Absent is an answer. A run with no kill in it did not redeliver anything,
  // and a zero-filled record would draw a redelivery that never happened.
  it('reports none when the run had none', () => {
    expect(describeRun({ status: 200, body: body() }).redelivery).toBe(null)
  })

  it('turns the wire record into what the drawing reads', () => {
    const out = describeRun({
      status: 200,
      body: body({
        redelivery: {
          seq: 490_094, killedWorker: 1, toWorker: 3, delivery: 2,
          waitedMs: 30_005, ackWait: '30s', foldAt: 499_994, ranOn: 9_900,
          outcome: 'dropped',
        },
      }),
    })

    expect(out.redelivery).toMatchObject({
      killSeq: 490_094,
      killedWorker: 1,
      toWorker: 3,
      delivery: 2,
      waitedSeconds: 30.005,
      ackWait: '30s',
      foldAt: 499_994,
      ranOn: 9_900,
      recovered: false,
    })
  })

  // Two inputs, because "recovered: false" written once is a constant.
  it('says a redelivery that the fold accepted was recovered', () => {
    const out = describeRun({
      status: 200,
      body: body({
        redelivery: { seq: 94, killedWorker: 2, toWorker: 1, delivery: 2,
          waitedMs: 5_001, ackWait: '5s', foldAt: 93, ranOn: 0, outcome: 'folded' },
      }),
    })
    expect(out.redelivery.recovered).toBe(true)
    expect(out.redelivery.waitedSeconds).toBe(5.001)
  })
})
