import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { validateManifest } from '../registry/manifestSchema.js'
import { PLUGIN_STATUS, PluginStatusRecord } from '../registry/pluginStatus.js'
import { RemoteAllowlist } from '../registry/remoteAllowlist.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { createPluginLoader, DEFAULT_TIMEOUTS } from './pluginLoader.js'

/*
  BR-AS04, BR-AS08 — a remote that never answers is a failure, not a spinner.

  A `type: 'module'` remote is fetched through a bare `import()`, which the
  Module Federation runtime does not time. These specs hold the loader's own
  bound to three promises:

  * a timeout ends the attempt with its own cause, and clears the timer and
    the in-flight entry, so Retry starts a fresh attempt;
  * work that finishes after its timeout publishes nothing and cannot replace
    a newer attempt's state — the timeout stopped the waiting, not the work;
  * an activate() that timed out is never called a second time on the same
    module: a retry waits on the first call instead.
*/

const LOAD_MS = 1000
const ACTIVATE_MS = 500

const plugin = () =>
  validateManifest({
    id: 'example-plugin',
    name: 'Example Plugin',
    schemaVersion: REGISTRY_SCHEMA_VERSION,
    shellApiVersion: SHELL_API_VERSION,
    remote: { kind: 'federated', url: 'http://localhost:7110/remoteEntry.js', module: './plugin' },
    contributions: [
      { kind: 'route', id: 'vessels', path: '/example-plugin/vessels', title: 'Vessels' },
    ],
  }).plugin

/* A promise the spec settles by hand, for a remote or an activate() that is
   slow on purpose. */
const deferred = () => {
  let resolve
  let reject
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

/* `answers` is one entry per adapter.load call: a module, or a deferred
   whose promise is returned as it stands. */
const harness = (answers) => {
  const p = plugin()
  const allowlist = new RemoteAllowlist()
  allowlist.add(p)
  const record = new PluginStatusRecord(p.id)
  record.transition(PLUGIN_STATUS.AVAILABLE)
  const adapter = {
    load: vi.fn(async () => {
      const answer = answers.shift()
      return answer?.promise ? answer.promise : answer
    }),
  }
  const loader = createPluginLoader({
    allowlist,
    adapters: { federated: adapter },
    statuses: new Map([[p.id, record]]),
    timeouts: { load: LOAD_MS, activate: ACTIVATE_MS },
  })
  return { loader, plugin: p, record, adapter }
}

/* Start a load and observe how it settles, without letting a rejection go
   unhandled while fake time is advanced. */
const start = (loader, p) => {
  const outcome = { state: 'pending', value: null }
  loader.load(p).then(
    (value) => Object.assign(outcome, { state: 'resolved', value }),
    (error) => Object.assign(outcome, { state: 'rejected', value: error }),
  )
  return outcome
}

beforeEach(() => { vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers() })

describe('a remote that never answers', () => {
  it('fails as load-timeout once the bound passes, and leaves no timer behind', async () => {
    const { loader, plugin: p, record } = harness([deferred()])

    const outcome = start(loader, p)
    await vi.advanceTimersByTimeAsync(LOAD_MS - 1)
    expect(outcome.state).toBe('pending')
    expect(record.status).toBe(PLUGIN_STATUS.LOADING)

    await vi.advanceTimersByTimeAsync(1)

    expect(outcome.state).toBe('rejected')
    expect(record.status).toBe(PLUGIN_STATUS.FAILED)
    expect(record.reasonCode).toBe('load-timeout')
    expect(vi.getTimerCount()).toBe(0)
  })

  it('lets Retry start a fresh attempt, which can succeed', async () => {
    const activate = vi.fn()
    const { loader, plugin: p, record, adapter } = harness([deferred(), { activate }])

    start(loader, p)
    await vi.advanceTimersByTimeAsync(LOAD_MS)

    const retry = start(loader, p)
    await vi.advanceTimersByTimeAsync(0)

    expect(adapter.load).toHaveBeenCalledTimes(2)
    expect(retry.state).toBe('resolved')
    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
    expect(activate).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('leaves no timer behind a load that finishes in time', async () => {
    const { loader, plugin: p, record } = harness([{ activate: vi.fn() }])

    await loader.load(p)

    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
    expect(vi.getTimerCount()).toBe(0)
  })
})

describe('the timed-out attempt finishes late', () => {
  it('publishes nothing when there has been no retry', async () => {
    const stalled = deferred()
    const lateActivate = vi.fn()
    const { loader, plugin: p, record } = harness([stalled])

    start(loader, p)
    await vi.advanceTimersByTimeAsync(LOAD_MS)
    stalled.resolve({ activate: lateActivate })
    await vi.advanceTimersByTimeAsync(0)

    expect(record.status).toBe(PLUGIN_STATUS.FAILED)
    expect(record.reasonCode).toBe('load-timeout')
    expect(loader.isLoaded(p.id)).toBe(false)
    expect(loader.peek(p.id)).toBeNull()
    expect(lateActivate).not.toHaveBeenCalled()
  })

  it('does not replace the module a newer attempt already published', async () => {
    const stalled = deferred()
    const lateModule = { activate: vi.fn() }
    const freshModule = { activate: vi.fn() }
    const { loader, plugin: p, record } = harness([stalled, freshModule])

    start(loader, p)
    await vi.advanceTimersByTimeAsync(LOAD_MS)
    await loader.load(p)
    stalled.resolve(lateModule)
    await vi.advanceTimersByTimeAsync(0)

    expect(loader.peek(p.id)).toBe(freshModule)
    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
    expect(lateModule.activate).not.toHaveBeenCalled()
  })

  it('does not disturb a newer attempt that is still loading', async () => {
    const stalled = deferred()
    const newer = deferred()
    const freshModule = { activate: vi.fn() }
    const { loader, plugin: p, record } = harness([stalled, newer])

    start(loader, p)
    await vi.advanceTimersByTimeAsync(LOAD_MS)
    const retry = start(loader, p)
    await vi.advanceTimersByTimeAsync(0)

    stalled.resolve({ activate: vi.fn() })
    await vi.advanceTimersByTimeAsync(0)
    expect(record.status).toBe(PLUGIN_STATUS.LOADING)
    expect(loader.isLoaded(p.id)).toBe(false)

    newer.resolve(freshModule)
    await vi.advanceTimersByTimeAsync(0)
    expect(retry.state).toBe('resolved')
    expect(loader.peek(p.id)).toBe(freshModule)
    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
  })

  it('does not count a late rejection as a second failure', async () => {
    const stalled = deferred()
    const { loader, plugin: p, record } = harness([stalled, { activate: vi.fn() }])

    start(loader, p)
    await vi.advanceTimersByTimeAsync(LOAD_MS)
    await loader.load(p)
    stalled.reject(new Error('chunk-load-failed'))
    await vi.advanceTimersByTimeAsync(0)

    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
  })
})

describe('an activate() that does not return', () => {
  it('fails as activate-timeout and does not publish the module', async () => {
    const started = deferred()
    const module = { activate: vi.fn(() => started.promise) }
    const { loader, plugin: p, record } = harness([module])

    const outcome = start(loader, p)
    await vi.advanceTimersByTimeAsync(ACTIVATE_MS)

    expect(outcome.state).toBe('rejected')
    expect(record.status).toBe(PLUGIN_STATUS.FAILED)
    expect(record.reasonCode).toBe('activate-timeout')
    expect(loader.isLoaded(p.id)).toBe(false)
    expect(vi.getTimerCount()).toBe(0)

    // The plugin's code was not cancelled and finishes. Still nothing published.
    started.resolve()
    await vi.advanceTimersByTimeAsync(0)
    expect(record.status).toBe(PLUGIN_STATUS.FAILED)
    expect(loader.isLoaded(p.id)).toBe(false)
  })

  it('is not called again when a retry gets the same module back', async () => {
    const started = deferred()
    const module = { activate: vi.fn(() => started.promise) }
    // The federation runtime caches a remote: both loads answer the same object.
    const { loader, plugin: p, record } = harness([module, module])

    start(loader, p)
    await vi.advanceTimersByTimeAsync(ACTIVATE_MS)
    const retry = start(loader, p)
    await vi.advanceTimersByTimeAsync(0)
    started.resolve()
    await vi.advanceTimersByTimeAsync(0)

    expect(module.activate).toHaveBeenCalledTimes(1)
    expect(retry.state).toBe('resolved')
    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
    expect(loader.peek(p.id)).toBe(module)
  })

  it('is called again on retry when the first call threw, as before', async () => {
    const module = {
      activate: vi.fn()
        .mockImplementationOnce(() => { throw new Error('boom') })
        .mockImplementationOnce(() => undefined),
    }
    const { loader, plugin: p, record } = harness([module, module])

    await expect(loader.load(p)).rejects.toThrow('boom')
    expect(record.reasonCode).toBe('activate-threw')
    await loader.load(p)

    expect(module.activate).toHaveBeenCalledTimes(2)
    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
  })
})

describe('BR-AS56 — withdrawn while the load is stalled', () => {
  it('settles the timeout as the withdrawal\'s business and stays withdrawn', async () => {
    const { loader, plugin: p, record } = harness([deferred()])

    const outcome = start(loader, p)
    record.withdraw()
    await vi.advanceTimersByTimeAsync(LOAD_MS)

    expect(outcome.state).toBe('rejected')
    expect(record.status).toBe(PLUGIN_STATUS.WITHDRAWN)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('returns as failed, and a retry after the return can succeed', async () => {
    const { loader, plugin: p, record } = harness([deferred(), { activate: vi.fn() }])

    start(loader, p)
    record.withdraw()
    await vi.advanceTimersByTimeAsync(LOAD_MS)
    record.restore()
    expect(record.status).toBe(PLUGIN_STATUS.FAILED)

    await loader.load(p)
    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
  })

  it('refuses a retry while the plugin is still withdrawn', async () => {
    const { loader, plugin: p, record, adapter } = harness([deferred()])

    start(loader, p)
    record.withdraw()
    await vi.advanceTimersByTimeAsync(LOAD_MS)

    await expect(loader.load(p)).rejects.toThrow('withdrawn')
    expect(adapter.load).toHaveBeenCalledTimes(1)
  })
})

describe('the default bounds', () => {
  it('leave room for example-plugin-slow\'s deliberate six-second load', () => {
    expect(DEFAULT_TIMEOUTS.load).toBeGreaterThan(6000)
    expect(DEFAULT_TIMEOUTS.activate).toBeGreaterThan(0)
  })
})
