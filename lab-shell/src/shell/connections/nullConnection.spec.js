/*
  Phase 16, decision 3 — the null connection has the real surface, so
  `createRegistrySession` runs unmodified in both modes.

  The interesting assertion is not the shape of the object; it is the last
  describe block, where the REAL session is driven over it and reads a
  catalogue with no broker anywhere.
*/

import { describe, expect, it, vi } from 'vitest'

import { createNullConnection } from './nullConnection.js'
import { createShellConnection } from './shellConnection.js'
import { createRegistrySession } from '../registry/registrySession.js'

const SURFACE = ['state', 'subscribe', 'request', 'start', 'flush', 'close']

describe('decision 3 — the surface is the real one', () => {
  it('offers every member a consumer of the real connection uses', () => {
    const connection = createNullConnection()
    for (const member of SURFACE) expect(connection[member]).toBeDefined()
    expect(connection.state.epoch).toBe(0)
  })

  it('matches the real connection member for member', () => {
    const real = createShellConnection({ connect: vi.fn() })
    const nul = createNullConnection()
    for (const member of SURFACE) expect(typeof nul[member]).toBe(typeof real[member])
  })

  it('emits one epoch on start, which is the session\'s establish signal', async () => {
    const connection = createNullConnection()
    await connection.start()
    expect(connection.state.epoch).toBe(1)
    await connection.start()
    expect(connection.state.epoch).toBe(1)
  })

  it('does not report `connected`, because there is nothing to be up or down', () => {
    // ShellFooter watches for `connected === false` before saying "connection
    // offline · registry may be out of date". Undefined keeps it quiet, which
    // is right: a build catalogue cannot go out of date.
    expect(createNullConnection().state.connected).toBeUndefined()
  })

  it('rejects a request rather than answering one', async () => {
    await expect(createNullConnection().request('any.subject', {})).rejects.toThrow('connection-unavailable')
  })

  it('returns a real subscription handle that can be unsubscribed', () => {
    const sub = createNullConnection().subscribe('any.subject', vi.fn())
    expect(() => sub.unsubscribe()).not.toThrow()
  })

  it('stays quiet after close', async () => {
    const connection = createNullConnection()
    await connection.close()
    await connection.start()
    expect(connection.state.epoch).toBe(0)
  })
})

describe('decision 3 — the unmodified session runs over it', () => {
  it('reads the catalogue with no broker, no dialer and no credential mint', async () => {
    const connection = createNullConnection()
    const fetchRegistry = vi.fn(async () => ({ ok: true, revision: 1, plugins: [], degraded: false }))
    const onResult = vi.fn()

    const session = createRegistrySession({
      connection,
      client: { fetchRegistry },
      shell: { registry: { heldRevision: null, revision: null } },
      onResult,
      afterPaint: async () => {},
    })

    await session.start()
    await vi.waitFor(() => expect(onResult).toHaveBeenCalled())

    // Boot read, then the read that closes the read-to-subscribe gap. Same
    // two calls the real connection produces — the session did not branch.
    expect(fetchRegistry).toHaveBeenCalledWith({ heldRevision: null })
    expect(onResult.mock.calls[0][0]).toMatchObject({ ok: true, reason: 'boot' })

    await session.stop()
  })
})
