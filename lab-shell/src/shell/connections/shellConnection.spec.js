/*
  BR-AS30 / decision 54 — the boot read is off the critical path.

  The shell's own plugins are in its own bundle. Nothing about painting them
  depends on a socket, so nothing about painting them may WAIT on one: the
  transport is started after the first paint, and a transport that never
  arrives costs the remote catalog and nothing else.

  These specs are written against a connection whose `connect` is a promise
  the test holds open, because "before it resolved" is the only interesting
  moment and a resolved-by-then stub cannot express it.
*/

import { describe, expect, it, vi } from 'vitest'

import { createShellConnection } from './shellConnection.js'

const deferred = () => {
  let resolve
  let reject
  const promise = new Promise((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

const stubConn = () => ({ close: vi.fn(), isClosed: () => false })

describe('BR-AS30 — the shell paints before it connects', () => {
  it('is usable the moment it is created, with no connection yet', () => {
    const gate = deferred()
    const shellConn = createShellConnection({ connect: () => gate.promise })

    // Not started, not connected, not throwing. A caller can read this state
    // and render from it on the first tick.
    expect(shellConn.state.connected).toBe(false)
    expect(shellConn.state.epoch).toBe(0)
    expect(shellConn.state.error).toBe(null)
  })

  it('start() does not block on the socket', async () => {
    const gate = deferred()
    const shellConn = createShellConnection({ connect: () => gate.promise })

    const started = shellConn.start()
    // Still not connected while `connect` is outstanding — the shell has
    // already returned to its caller by here.
    expect(shellConn.state.connected).toBe(false)

    gate.resolve(stubConn())
    await started
    expect(shellConn.state.connected).toBe(true)
  })

  /* The failure case is the one that matters: a shell that rejected here
     would take the whole app down over a service it is explicitly allowed to
     run without (BR-AS04, BR-AS22). */
  it('records a refused connection instead of throwing it', async () => {
    const shellConn = createShellConnection({
      connect: () => Promise.reject(new Error('no route to host')),
    })

    await expect(shellConn.start()).resolves.toBeDefined()
    expect(shellConn.state.connected).toBe(false)
    expect(shellConn.state.error).not.toBe(null)
  })

  /* BR-AS04: whatever the transport says on the way down, the shell keeps a
     code — never a host, port or credential — because this string reaches
     the footer. */
  it('keeps a code and no address from a refusal', async () => {
    const shellConn = createShellConnection({
      connect: () => Promise.reject(new Error('dial tcp 127.0.0.1:4222: connect: refused')),
    })
    await shellConn.start()

    expect(shellConn.state.error.code).toBeTruthy()
    const rendered = JSON.stringify(shellConn.state.error)
    expect(rendered).not.toContain('4222')
    expect(rendered).not.toContain('127.0.0.1')
  })

  /* Decision 56's counter, and the Admin UI's precedent: every socket
     re-establishment is a new epoch, because that is the only thing the shell
     can key "re-read unconditionally" off — core NATS has no gap detection
     and no redelivery. */
  it('bumps the epoch on every re-establishment, and never reuses one', async () => {
    let handler = null
    const shellConn = createShellConnection({
      connect: () => Promise.resolve(stubConn()),
      onReconnect: (fn) => {
        handler = fn
      },
    })
    await shellConn.start()
    expect(shellConn.state.epoch).toBe(1)

    handler()
    handler()
    expect(shellConn.state.epoch).toBe(3)
  })

  it('closes what it opened, and is safe to close twice', async () => {
    const conn = stubConn()
    const shellConn = createShellConnection({ connect: () => Promise.resolve(conn) })
    await shellConn.start()

    await shellConn.close()
    await shellConn.close()
    expect(conn.close).toHaveBeenCalledTimes(1)
    expect(shellConn.state.connected).toBe(false)
  })

  it('closing before the connection lands does not leave a socket open', async () => {
    const gate = deferred()
    const conn = stubConn()
    const shellConn = createShellConnection({ connect: () => gate.promise })

    const started = shellConn.start()
    await shellConn.close()
    gate.resolve(conn)
    await started

    expect(conn.close).toHaveBeenCalled()
    expect(shellConn.state.connected).toBe(false)
  })
})
