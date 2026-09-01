import { describe, expect, it } from 'vitest'

import { PLUGIN_STATUS, PluginStatusRecord } from './pluginStatus.js'

/*
  Withdrawal is not a transition in the table (BR-AS56). It arrives from the
  registry rather than from the plugin's own progress, it can land on any
  placed status, and the status it lands on is the one a return must go back
  to — so it is its own pair of verbs, and the table stays a description of
  loading.
*/

const active = () => {
  const record = new PluginStatusRecord('fleet-ops')
  record.transition(PLUGIN_STATUS.AVAILABLE)
  record.transition(PLUGIN_STATUS.LOADING)
  record.transition(PLUGIN_STATUS.ACTIVE)
  return record
}

describe('BR-AS56 — a withdrawn plugin has its own status', () => {
  it('withdraws an active plugin', () => {
    const record = active()

    expect(record.withdraw()).toBe(true)

    expect(record.status).toBe(PLUGIN_STATUS.WITHDRAWN)
    expect(record.reasonCode).toBe('publisher-withdrawn')
    expect(record.history).toContain(PLUGIN_STATUS.WITHDRAWN)
  })

  it('is safe to withdraw twice, and remembers the first status', () => {
    const record = active()

    record.withdraw()
    expect(record.withdraw()).toBe(false)

    record.restore()
    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
  })

  it('leaves a plugin nobody placed alone', () => {
    const record = new PluginStatusRecord('fleet-ops')
    record.transition(PLUGIN_STATUS.DISABLED, { code: 'operator-disabled' })

    expect(record.withdraw()).toBe(false)
    expect(record.status).toBe(PLUGIN_STATUS.DISABLED)
  })

  it('refuses an ordinary transition out of withdrawn', () => {
    const record = active()
    record.withdraw()

    expect(() => record.transition(PLUGIN_STATUS.ACTIVE)).toThrow(/withdrawn/)
  })

  it('cannot be loaded while withdrawn', () => {
    const record = active()
    record.withdraw()

    expect(record.isPlaced).toBe(false)
  })
})

describe('BR-AS59 — a return goes back to where it was', () => {
  it('restores an active plugin to active, not to available', () => {
    const record = active()
    record.withdraw()

    expect(record.restore()).toBe(true)

    // Straight back to active: the module is still loaded and activate() must
    // not be called a second time.
    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
    expect(record.reasonCode).toBeNull()
  })

  it('restores a never-loaded plugin to available, so it stays lazy', () => {
    const record = new PluginStatusRecord('fleet-ops')
    record.transition(PLUGIN_STATUS.AVAILABLE)
    record.withdraw()

    record.restore()

    expect(record.status).toBe(PLUGIN_STATUS.AVAILABLE)
  })

  it('is a no-op for a plugin that was never withdrawn', () => {
    const record = active()

    expect(record.restore()).toBe(false)
    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
  })
})
