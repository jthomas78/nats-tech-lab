import { describe, expect, it, vi } from 'vitest'

import { createRegistryWatcher, DEFAULT_INTERVAL_MS } from './registryWatcher.js'

/*
  BR-AS19's trigger half (decision 44). What matters here is not that a timer
  exists but that the read is CONDITIONAL and that a hidden tab is quiet: a
  shell that re-read unconditionally every ten minutes would ship the whole
  document to learn nothing, and one that polled in the background would do it
  for a screen nobody is looking at.
*/

function harness({ results = [], visibility = 'visible', intervalMs } = {}) {
  const listeners = new Map()
  const doc = {
    visibilityState: visibility,
    addEventListener: (name, fn) => listeners.set(name, fn),
    removeEventListener: (name) => listeners.delete(name),
  }
  const timers = { setInterval: vi.fn(() => 'timer-1'), clearInterval: vi.fn() }
  const queue = [...results]
  const client = {
    fetchRegistry: vi.fn(async () => queue.shift() ?? { ok: true, unchanged: true }),
  }
  const onResult = vi.fn()
  const watcher = createRegistryWatcher({ client, onResult, etag: '"7"', doc, timers, intervalMs })
  return { watcher, client, onResult, timers, doc, fire: (name) => listeners.get(name)?.() }
}

describe('BR-AS19 — the shell re-reads the registry it is already running', () => {
  it('asks conditionally with the revision it is holding', async () => {
    const h = harness()
    await h.watcher.check()
    expect(h.client.fetchRegistry).toHaveBeenCalledWith({ etag: '"7"' })
  })

  it('reports an unchanged registry as a result, not as a failure', async () => {
    const h = harness({ results: [{ ok: true, unchanged: true, etag: '"7"' }] })
    await h.watcher.check()
    expect(h.onResult.mock.calls[0][0]).toMatchObject({ ok: true, unchanged: true })
  })

  it('carries the new revision into the next read', async () => {
    const h = harness({ results: [{ ok: true, unchanged: false, etag: '"9"', plugins: [] }] })
    await h.watcher.check()
    await h.watcher.check()
    expect(h.client.fetchRegistry.mock.calls[1][0]).toEqual({ etag: '"9"' })
  })

  it('keeps the last good revision when a read fails, so the next one is still conditional', async () => {
    const h = harness({ results: [{ ok: false, code: 'registry-unreachable', message: 'x' }] })
    await h.watcher.check()
    await h.watcher.check()
    expect(h.client.fetchRegistry.mock.calls[1][0]).toEqual({ etag: '"7"' })
  })

  it('reads when the tab comes back into view', async () => {
    const h = harness()
    h.watcher.start()
    h.fire('visibilitychange')
    await Promise.resolve()
    expect(h.client.fetchRegistry).toHaveBeenCalledTimes(1)
  })

  it('reads nothing on an interval tick while the tab is hidden', async () => {
    const h = harness({ visibility: 'hidden' })
    h.watcher.start()
    const tick = h.timers.setInterval.mock.calls[0][0]
    tick()
    await Promise.resolve()
    expect(h.client.fetchRegistry).not.toHaveBeenCalled()
  })

  it('runs the interval slowly by default — focus is the responsive trigger', () => {
    const h = harness()
    h.watcher.start()
    expect(h.timers.setInterval.mock.calls[0][1]).toBe(DEFAULT_INTERVAL_MS)
  })

  it('collapses a focus read and an interval tick that land together into one request', async () => {
    const h = harness()
    const both = Promise.all([h.watcher.check('visible'), h.watcher.check('interval')])
    await both
    expect(h.client.fetchRegistry).toHaveBeenCalledTimes(1)
  })

  it('stops listening and stops the interval on stop()', () => {
    const h = harness()
    h.watcher.start().stop()
    expect(h.timers.clearInterval).toHaveBeenCalledWith('timer-1')
    h.fire('visibilitychange')
    expect(h.client.fetchRegistry).not.toHaveBeenCalled()
  })
})
