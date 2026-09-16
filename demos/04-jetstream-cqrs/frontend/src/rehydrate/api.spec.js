import { describe, expect, it, vi } from 'vitest'

import { describeResult, fetchBoth, fetchRehydration, unreachable } from './api.js'

function jsonResponse(status, body) {
  return { status, json: async () => body }
}

const okBody = {
  id: 'V1',
  usedSnapshot: false,
  fromSeq: 1,
  eventsRead: 10001,
  lastSeq: 10003,
  elapsedMs: 25.4,
  status: 'registered',
  plate: 'ABC-123',
}

describe('describeResult', () => {
  it('reads one half of the comparison out of a 200', () => {
    const r = describeResult({ snapshot: false, id: 'V1', status: 200, body: okBody })
    expect(r.kind).toBe('ok')
    expect(r.eventsRead).toBe(10001)
    expect(r.elapsedMs).toBeCloseTo(25.4)
    expect(r.status).toBe('registered')
  })

  // No rule code, ever. A rehydration judges no command.
  it('calls anything else broken, and never a refusal', () => {
    const r = describeResult({
      snapshot: true,
      id: 'V1',
      status: 502,
      body: { error: 'Unavailable', message: 'nats down' },
    })
    expect(r.kind).toBe('broken')
    expect(r.rule).toBeUndefined()
    expect(r.message).toBe('nats down')
  })
})

describe('unreachable', () => {
  it('names the shim, so the fix is obvious', () => {
    expect(unreachable({ snapshot: false, id: 'V1', reason: 'failed' }).message).toContain(
      'cqrs serve',
    )
  })
})

describe('fetchRehydration', () => {
  it('always spells the mode out, never leaving it to the default', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse(200, okBody))
    await fetchRehydration('V1', false, { fetchImpl, base: 'http://x' })
    expect(fetchImpl.mock.calls[0][0]).toBe('http://x/rehydrate?id=V1&snapshot=false')

    await fetchRehydration('V1', true, { fetchImpl, base: 'http://x' })
    expect(fetchImpl.mock.calls[1][0]).toBe('http://x/rehydrate?id=V1&snapshot=true')
  })

  it('escapes an id rather than building a broken URL', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse(200, okBody))
    await fetchRehydration('a b&c', true, { fetchImpl, base: 'http://x' })
    expect(fetchImpl.mock.calls[0][0]).toContain('id=a%20b%26c')
  })

  it('returns an outcome when fetch itself throws, never an exception', async () => {
    const fetchImpl = vi.fn(async () => {
      throw new Error('connection refused')
    })
    const r = await fetchRehydration('V1', true, { fetchImpl })
    expect(r.kind).toBe('broken')
    expect(r.message).toContain('connection refused')
  })
})

describe('fetchBoth', () => {
  it('runs the cold side first, so the snapshot side is not flattered', async () => {
    const seen = []
    const fetchImpl = vi.fn(async (url) => {
      seen.push(url.includes('snapshot=true'))
      return jsonResponse(200, okBody)
    })
    await fetchBoth('V1', { fetchImpl, base: 'http://x' })
    expect(seen).toEqual([false, true])
  })

  // Two rehydrations in flight measure each other as well as themselves.
  it('runs them one at a time, not together', async () => {
    let inFlight = 0
    let overlapped = false
    const fetchImpl = vi.fn(async () => {
      inFlight += 1
      if (inFlight > 1) overlapped = true
      await Promise.resolve()
      inFlight -= 1
      return jsonResponse(200, okBody)
    })
    await fetchBoth('V1', { fetchImpl, base: 'http://x' })
    expect(overlapped).toBe(false)
  })

  it('hands back both halves', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse(200, okBody))
    const { cold, warm } = await fetchBoth('V1', { fetchImpl, base: 'http://x' })
    expect(cold.kind).toBe('ok')
    expect(warm.kind).toBe('ok')
  })
})
