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
