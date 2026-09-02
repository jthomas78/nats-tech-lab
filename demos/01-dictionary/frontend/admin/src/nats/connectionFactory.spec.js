// Reconnect-lifecycle coverage for the shared connect/subscribe machinery.
//
// Both behaviors here were found by driving a running stack, not by reading
// the code, and both were invisible to the composable-level specs because
// those mock this factory out entirely:
//
//   - `connected` alone cannot tell a consumer when to resync, because
//     nats-core absorbs most reconnects internally and never flips it. That is
//     what `epoch` is for.
//   - a single non-retrying reconnect attempt leaves the app permanently dark,
//     since by the time closed() resolves the server is usually still down.
import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const wsconnect = vi.fn()
vi.mock('@nats-io/nats-core', () => ({
  wsconnect: (...args) => wsconnect(...args),
  jwtAuthenticator: () => () => ({}),
  headers: () => new Map(),
}))

import { createConnectionState } from './connectionFactory.js'

// A fake NatsConnection whose status()/closed() we drive by hand.
function fakeConn() {
  let emitStatus
  let resolveClosed
  const statuses = []
  const conn = {
    closed: () => new Promise((r) => { resolveClosed = r }),
    status: () => ({
      [Symbol.asyncIterator]() {
        return {
          next: () => new Promise((r) => {
            if (statuses.length) return r({ value: statuses.shift(), done: false })
            emitStatus = (v) => r({ value: v, done: false })
          }),
        }
      },
    }),
    close: vi.fn(),
    subscribe: vi.fn(),
  }
  return {
    conn,
    emit: (type) => { if (emitStatus) emitStatus({ type }); else statuses.push({ type }) },
    close: (err) => resolveClosed?.(err),
  }
}

const fetchConnectInfo = vi.fn(async () => ({ wsUrl: 'ws://x', jwt: 'j', nkeySeed: 's' }))

function makeState() {
  return createConnectionState({ fetchConnectInfo, connectionName: 'test-conn' })
}

describe('createConnectionState reconnect lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useRealTimers()
  })

  it('bumps epoch on the initial connect', async () => {
    const f = fakeConn()
    wsconnect.mockResolvedValue(f.conn)
    const state = makeState()
    expect(state.epoch.value).toBe(0)

    await state.connect()

    expect(state.connected.value).toBe(true)
    expect(state.epoch.value).toBe(1)
  })

  // The regression the whole change exists for: nats-core re-dials on the SAME
  // connection, closed() never resolves, and a consumer watching `connected`
  // alone never learns it needs to resync.
  it('bumps epoch on an internal reconnect, which closed() never reports', async () => {
    const f = fakeConn()
    wsconnect.mockResolvedValue(f.conn)
    const state = makeState()
    await state.connect()
    await flushPromises()

    f.emit('disconnect')
    await flushPromises()
    expect(state.connected.value).toBe(false) // honest while re-dialling

    f.emit('reconnect')
    await flushPromises()

    expect(state.connected.value).toBe(true)
    expect(state.epoch.value).toBe(2)
    expect(wsconnect).toHaveBeenCalledTimes(1) // same socket — no outer reconnect
  })

  it('keeps retrying after a failed reconnect instead of giving up permanently', async () => {
    const first = fakeConn()
    const revived = fakeConn()
    wsconnect
      .mockResolvedValueOnce(first.conn)
      .mockRejectedValueOnce(new Error('server down'))
      .mockResolvedValueOnce(revived.conn)

    const state = makeState()
    await state.connect()
    await flushPromises()

    first.close(new Error('lost'))
    await vi.waitFor(() => expect(state.connected.value).toBe(false), { timeout: 5000 })
    await vi.waitFor(() => expect(state.connected.value).toBe(true), { timeout: 5000 })

    expect(wsconnect).toHaveBeenCalledTimes(3) // initial + failed retry + success
    expect(state.epoch.value).toBe(2)
  })

  it('stops retrying once a caller disconnects on purpose', async () => {
    const f = fakeConn()
    wsconnect.mockResolvedValueOnce(f.conn).mockRejectedValue(new Error('server down'))
    const state = makeState()
    await state.connect()
    await flushPromises()

    await state.disconnect()
    f.close(new Error('lost'))
    await flushPromises()

    expect(state.connected.value).toBe(false)
    expect(wsconnect).toHaveBeenCalledTimes(1) // never retried
  })

  it('preserves the shared conflict envelope and registry revision details', async () => {
    const f = fakeConn()
    const body = { error: 'registry moved', conflict: true, code: 'stale-revision', currentRevision: 9, yourRevision: 4 }
    f.conn.request = vi.fn(async () => ({ data: new TextEncoder().encode(JSON.stringify(body)) }))
    wsconnect.mockResolvedValue(f.conn)
    const state = makeState()
    await state.connect()
    await expect(state.request('api._platform.mfe-registry.entries.upsert.v1', {})).rejects.toMatchObject({ conflict: true, code: 'stale-revision', body })
    await state.disconnect()
  })
})
