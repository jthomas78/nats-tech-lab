/*
  BR-AS60 — one nav item, one mark.

  These run against the rule itself rather than against a mounted App.vue: the
  precedence used to be split between a helper and the ORDER of two template
  conditionals, and the template half was only ever checkable by rendering it.
*/
import { describe, expect, it } from 'vitest'

import { HEALTH_STATE } from './healthPlane.js'
import { navMark } from './navMark.js'
import { PLUGIN_STATUS } from './pluginStatus.js'

const health = (frontend, backend = HEALTH_STATE.HEALTHY) => ({
  frontend: { state: frontend, cause: '' },
  backend: { state: backend, cause: '' },
})

describe('a plugin the shell believes is fine', () => {
  it('draws nothing — a dot that is always there stops being a signal', () => {
    expect(navMark({ status: PLUGIN_STATUS.ACTIVE, health: health(HEALTH_STATE.HEALTHY) })).toBeNull()
  })

  it('draws nothing for a plugin nobody has looked at yet', () => {
    expect(navMark({ status: PLUGIN_STATUS.AVAILABLE, health: null })).toBeNull()
  })

  it('draws nothing with no arguments at all', () => {
    expect(navMark()).toBeNull()
  })
})

describe('the plugin`s own status', () => {
  it('marks a failure in error tone, and names the status', () => {
    expect(navMark({ status: PLUGIN_STATUS.FAILED })).toEqual({
      tone: 'err',
      title: PLUGIN_STATUS.FAILED,
    })
  })

  it('marks an incompatible plugin in warning tone', () => {
    expect(navMark({ status: PLUGIN_STATUS.INCOMPATIBLE })).toEqual({
      tone: 'warn',
      title: PLUGIN_STATUS.INCOMPATIBLE,
    })
  })

  it('leaves a disabled plugin unmarked — that was the operator`s own decision', () => {
    expect(navMark({ status: PLUGIN_STATUS.DISABLED })).toBeNull()
  })

  it('leaves a loading plugin unmarked — the skeleton already says so', () => {
    expect(navMark({ status: PLUGIN_STATUS.LOADING })).toBeNull()
  })
})

describe('health decorates a plugin that loaded', () => {
  it('marks a plugin whose frontend dependency is unavailable', () => {
    expect(navMark({ status: PLUGIN_STATUS.ACTIVE, health: health(HEALTH_STATE.UNAVAILABLE) })).toEqual({
      tone: 'warn',
      title: 'a dependency is unavailable',
    })
  })

  it('marks a plugin whose backend dependency is unavailable', () => {
    const signals = health(HEALTH_STATE.HEALTHY, HEALTH_STATE.UNAVAILABLE)
    expect(navMark({ status: PLUGIN_STATUS.ACTIVE, health: signals })?.tone).toBe('warn')
  })

  it('says nothing about a reading it cannot currently trust', () => {
    // "We cannot tell" is not a problem to act on.
    expect(navMark({ status: PLUGIN_STATUS.ACTIVE, health: health(HEALTH_STATE.STALE) })).toBeNull()
    expect(navMark({ status: PLUGIN_STATUS.ACTIVE, health: health(HEALTH_STATE.UNKNOWN) })).toBeNull()
    expect(navMark({ status: PLUGIN_STATUS.ACTIVE, health: health(HEALTH_STATE.NOT_CONFIGURED) })).toBeNull()
  })
})

describe('precedence — a failure keeps the dot to itself', () => {
  it('shows the failure, not the dependency, when both are true', () => {
    const mark = navMark({ status: PLUGIN_STATUS.FAILED, health: health(HEALTH_STATE.UNAVAILABLE) })

    expect(mark).toEqual({ tone: 'err', title: PLUGIN_STATUS.FAILED })
  })

  it('shows the incompatibility over a dependency, too', () => {
    const mark = navMark({ status: PLUGIN_STATUS.INCOMPATIBLE, health: health(HEALTH_STATE.UNAVAILABLE) })

    expect(mark.title).toBe(PLUGIN_STATUS.INCOMPATIBLE)
  })

  it('never returns two marks — the nav item has room for one', () => {
    const mark = navMark({ status: PLUGIN_STATUS.FAILED, health: health(HEALTH_STATE.UNAVAILABLE) })

    expect(Object.keys(mark).sort()).toEqual(['title', 'tone'])
  })
})
