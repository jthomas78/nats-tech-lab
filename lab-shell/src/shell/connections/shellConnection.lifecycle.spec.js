import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createShellConnection } from './shellConnection.js'

function socket() {
  let emit
  let end
  const conn = {
    close: vi.fn(),
    subscribe: vi.fn(() => ({ unsubscribe: vi.fn() })),
    closed: () => new Promise((resolve) => { end = resolve }),
    status: () => ({ [Symbol.asyncIterator]: () => ({ next: () => new Promise((resolve) => { emit = (type) => resolve({ value: { type }, done: false }) }) }) }),
  }
  return { conn, emit: (type) => emit(type), end: () => end() }
}
afterEach(() => vi.useRealTimers())

describe('BR-AS29 — socket lifecycle drives epochs', () => {
  it('does not call browser timers with a foreign receiver', async () => {
    let retry
    vi.stubGlobal('setTimeout', function (callback) {
      if (this !== undefined && this !== globalThis) throw new TypeError('Illegal invocation')
      retry = callback
      return 1
    })
    vi.stubGlobal('clearTimeout', function () {
      if (this !== undefined && this !== globalThis) throw new TypeError('Illegal invocation')
    })
    const connect = vi.fn().mockRejectedValueOnce(new Error('refused')).mockResolvedValue({ close() {} })
    const connection = createShellConnection({ connect })
    try {
      await expect(connection.start()).resolves.toBeDefined()
      retry()
      await connection.start()
      expect(connection.state.connected).toBe(true)
    } finally {
      vi.unstubAllGlobals()
      await connection.close()
    }
  })

  it('reports inner disconnect honestly and bumps epoch on recovery', async () => {
    const s = socket()
    const connection = createShellConnection({ connect: async () => s.conn })
    await connection.start()
    s.emit('disconnect')
    await flushPromises()
    expect(connection.state.connected).toBe(false)
    s.emit('reconnect')
    await flushPromises()
    expect(connection.state.epoch).toBe(2)
    expect(connection.state.connected).toBe(true)
    await connection.close()
  })

  it('remints after terminal close, restores subscriptions, and delivers malformed hints', async () => {
    vi.useFakeTimers()
    const a = socket(), b = socket()
    const connect = vi.fn().mockResolvedValueOnce(a.conn).mockResolvedValueOnce(b.conn)
    const connection = createShellConnection({ connect })
    await connection.start()
    const handler = vi.fn()
    const sub = connection.subscribe('notify.test', handler)
    a.end()
    await vi.advanceTimersByTimeAsync(1000)
    expect(connect).toHaveBeenCalledTimes(2)
    expect(connection.state.epoch).toBe(2)
    expect(b.conn.subscribe).toHaveBeenCalledOnce()
    b.conn.subscribe.mock.calls[0][1].callback(null, { data: new TextEncoder().encode('broken') })
    expect(handler).toHaveBeenCalledWith(null)
    sub.unsubscribe()
    await connection.close()
  })

  it('cancels initial-failure retries when disposed', async () => {
    vi.useFakeTimers()
    const connect = vi.fn().mockRejectedValue(new Error('refused'))
    const connection = createShellConnection({ connect })
    await connection.start()
    await connection.close()
    await vi.advanceTimersByTimeAsync(2000)
    expect(connect).toHaveBeenCalledOnce()
  })
})
