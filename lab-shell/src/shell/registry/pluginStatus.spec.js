import { describe, expect, it } from 'vitest'

import { canTransition, PLUGIN_STATUS, PluginStatusRecord } from './pluginStatus.js'

describe('BR-AS08 — metadata is available before code', () => {
  it('separates available from active, so a placed-but-unloaded plugin is observable', () => {
    const record = new PluginStatusRecord('example-plugin')
    record.transition(PLUGIN_STATUS.AVAILABLE)

    expect(record.status).toBe(PLUGIN_STATUS.AVAILABLE)
    expect(record.isPlaced).toBe(true)
  })

  it('reaches active only by way of loading', () => {
    expect(canTransition(PLUGIN_STATUS.AVAILABLE, PLUGIN_STATUS.ACTIVE)).toBe(false)
    expect(canTransition(PLUGIN_STATUS.AVAILABLE, PLUGIN_STATUS.LOADING)).toBe(true)
    expect(canTransition(PLUGIN_STATUS.LOADING, PLUGIN_STATUS.ACTIVE)).toBe(true)
  })

  it('records the whole path so the load can be asserted after the fact', () => {
    const record = new PluginStatusRecord('example-plugin')
    record.transition(PLUGIN_STATUS.AVAILABLE)
    record.transition(PLUGIN_STATUS.LOADING)
    record.transition(PLUGIN_STATUS.ACTIVE)

    expect(record.history).toEqual(['discovered', 'available', 'loading', 'active'])
  })
})

describe('BR-AS13 — incompatible is terminal', () => {
  it('cannot be loaded out of', () => {
    expect(canTransition(PLUGIN_STATUS.INCOMPATIBLE, PLUGIN_STATUS.LOADING)).toBe(false)
    expect(canTransition(PLUGIN_STATUS.INCOMPATIBLE, PLUGIN_STATUS.AVAILABLE)).toBe(false)
  })

  it('carries the rejection code that put it there', () => {
    const record = new PluginStatusRecord('old-plugin')
    record.transition(PLUGIN_STATUS.INCOMPATIBLE, {
      code: 'unsupported-shell-api-version',
      message: 'built against shell API 2',
    })

    expect(record.reasonCode).toBe('unsupported-shell-api-version')
    expect(record.reason).toContain('shell API 2')
  })
})

describe('BR-AS04 — failure is recoverable, not terminal', () => {
  it('can fail before loading, when the loader refuses it outright', () => {
    // An uncurated remote or a missing adapter is a failure of a plugin that
    // was never fetched. Making it pass through `loading` would report a fetch
    // that did not happen.
    expect(canTransition(PLUGIN_STATUS.AVAILABLE, PLUGIN_STATUS.FAILED)).toBe(true)
  })

  it('can fail from loading and from active', () => {
    expect(canTransition(PLUGIN_STATUS.LOADING, PLUGIN_STATUS.FAILED)).toBe(true)
    expect(canTransition(PLUGIN_STATUS.ACTIVE, PLUGIN_STATUS.FAILED)).toBe(true)
  })

  it('retries through loading rather than straight back to active', () => {
    expect(canTransition(PLUGIN_STATUS.FAILED, PLUGIN_STATUS.ACTIVE)).toBe(false)
    expect(canTransition(PLUGIN_STATUS.FAILED, PLUGIN_STATUS.LOADING)).toBe(true)
  })

  it('leaves a failed plugin placed, so its nav entry survives to be retried', () => {
    const record = new PluginStatusRecord('example-plugin')
    record.transition(PLUGIN_STATUS.AVAILABLE)
    record.transition(PLUGIN_STATUS.LOADING)
    record.transition(PLUGIN_STATUS.FAILED, { code: 'chunk-load-failed' })

    expect(record.isPlaced).toBe(true)
  })
})

describe('the machine refuses states the Plugins screen could not render', () => {
  it('will not make a disabled plugin available', () => {
    const record = new PluginStatusRecord('example-plugin')
    record.transition(PLUGIN_STATUS.DISABLED)

    expect(() => record.transition(PLUGIN_STATUS.AVAILABLE)).toThrow(/Illegal plugin status/)
  })

  it('will not make an incompatible plugin disabled as well', () => {
    const record = new PluginStatusRecord('old-plugin')
    record.transition(PLUGIN_STATUS.INCOMPATIBLE)

    expect(() => record.transition(PLUGIN_STATUS.DISABLED)).toThrow()
  })

  it('throws rather than recording, because an illegal transition is a shell bug', () => {
    const record = new PluginStatusRecord('example-plugin')

    expect(() => record.transition(PLUGIN_STATUS.ACTIVE)).toThrow()
    expect(record.status).toBe(PLUGIN_STATUS.DISCOVERED)
  })
})

describe('BR-AS56 / AS59 — work that finished after the withdrawal landed', () => {
  /* The rule used to live in the loader as two bare writes to `restoreTo`.
     It is a rule about the status machine, so it is stated and enforced here,
     and the loader now asks for it by name. */
  const withdrawnFrom = (status) => {
    const record = new PluginStatusRecord('example-plugin')
    record.transition(PLUGIN_STATUS.AVAILABLE)
    if (status !== PLUGIN_STATUS.AVAILABLE) record.transition(status)
    record.withdraw()
    return record
  }

  it('brings a load that succeeded back as active, so activate() is not called twice', () => {
    const record = withdrawnFrom(PLUGIN_STATUS.LOADING)

    expect(record.settleWhileWithdrawn(PLUGIN_STATUS.ACTIVE)).toBe(true)
    record.restore()

    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
  })

  it('brings a load that failed back as failed, not as ready to use', () => {
    const record = withdrawnFrom(PLUGIN_STATUS.LOADING)

    record.settleWhileWithdrawn(PLUGIN_STATUS.FAILED)
    record.restore()

    expect(record.status).toBe(PLUGIN_STATUS.FAILED)
  })

  it('overrides where the withdrawal came from, because the work outlived it', () => {
    // Withdrawn out of `loading`; returning to `loading` would wait on a fetch
    // that has already settled.
    const record = withdrawnFrom(PLUGIN_STATUS.LOADING)
    expect(record.restoreTo).toBe(PLUGIN_STATUS.LOADING)

    record.settleWhileWithdrawn(PLUGIN_STATUS.ACTIVE)

    expect(record.restoreTo).toBe(PLUGIN_STATUS.ACTIVE)
  })

  it('refuses a status a withdrawn plugin could not have reached', () => {
    const record = withdrawnFrom(PLUGIN_STATUS.LOADING)

    expect(() => record.settleWhileWithdrawn(PLUGIN_STATUS.LOADING)).toThrow(/cannot be settled/)
    expect(() => record.settleWhileWithdrawn(PLUGIN_STATUS.AVAILABLE)).toThrow(/cannot be settled/)
    expect(() => record.settleWhileWithdrawn(PLUGIN_STATUS.DISABLED)).toThrow(/cannot be settled/)
  })

  it('does nothing to a plugin that is not withdrawn', () => {
    // The loader calls this on any record it holds; a plugin still running
    // must not acquire a return it will never take.
    const record = new PluginStatusRecord('example-plugin')
    record.transition(PLUGIN_STATUS.AVAILABLE)

    expect(record.settleWhileWithdrawn(PLUGIN_STATUS.FAILED)).toBe(false)
    expect(record.restoreTo).toBeNull()
    expect(record.status).toBe(PLUGIN_STATUS.AVAILABLE)
  })
})
