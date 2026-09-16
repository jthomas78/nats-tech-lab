import { describe, expect, it } from 'vitest'

import { describePool, dropPool, fetchPool, seedPool } from './api.js'

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
