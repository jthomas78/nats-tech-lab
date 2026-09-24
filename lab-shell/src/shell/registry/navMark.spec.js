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
      description: PLUGIN_STATUS.FAILED,
    })
  })

  it('marks an incompatible plugin in warning tone', () => {
    expect(navMark({ status: PLUGIN_STATUS.INCOMPATIBLE })).toEqual({
      tone: 'warn',
      title: PLUGIN_STATUS.INCOMPATIBLE,
      description: PLUGIN_STATUS.INCOMPATIBLE,
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
      description: 'a dependency is unavailable',
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

    expect(mark).toEqual({
      tone: 'err',
      title: PLUGIN_STATUS.FAILED,
      description: PLUGIN_STATUS.FAILED,
    })
  })

  it('shows the incompatibility over a dependency, too', () => {
    const mark = navMark({ status: PLUGIN_STATUS.INCOMPATIBLE, health: health(HEALTH_STATE.UNAVAILABLE) })

    expect(mark.title).toBe(PLUGIN_STATUS.INCOMPATIBLE)
  })

  it('never returns two marks — the nav item has room for one', () => {
    const mark = navMark({ status: PLUGIN_STATUS.FAILED, health: health(HEALTH_STATE.UNAVAILABLE) })

    expect(Object.keys(mark).sort()).toEqual(['description', 'title', 'tone'])
  })
})

/*
  BR-AS89 / amendment A2 — the clash ranks LAST, and survives losing the dot.

  Load and health are runtime faults the reader is looking at now; a clash is
  a configuration fault an operator fixes. So a clash never takes the dot from
  either — which is exactly why it must not live in the dot alone.
*/
const clash = (message, kind = 'group-label-conflict') => ({ kind, message })

describe('BR-AS89 — a navigation clash', () => {
  it('marks an otherwise healthy entry in warning tone, and names the clash', () => {
    expect(navMark({ clashes: [clash('Navigation group ops is shown as "Ops"')] })).toEqual({
      tone: 'warn',
      title: 'Navigation group ops is shown as "Ops"',
      description: 'Navigation group ops is shown as "Ops"',
    })
  })

  it('is a configuration fault, never an error tone', () => {
    expect(navMark({ clashes: [clash('anything')] }).tone).toBe('warn')
  })

  it('draws nothing when the clash list is empty', () => {
    expect(navMark({ status: PLUGIN_STATUS.ACTIVE, clashes: [] })).toBeNull()
  })

  it('joins several clashes on one entry into one description', () => {
    const mark = navMark({ clashes: [clash('first'), clash('second', 'duplicate-item-label')] })

    expect(mark.description).toBe('first; second')
  })
})

describe('BR-AS89 — losing the dot does not lose the clash', () => {
  it('gives the dot to a failure, and keeps the clash in the description', () => {
    const mark = navMark({ status: PLUGIN_STATUS.FAILED, clashes: [clash('two plugins name this band')] })

    expect(mark.tone).toBe('err')
    expect(mark.title).toBe(PLUGIN_STATUS.FAILED)
    expect(mark.description).toBe(`${PLUGIN_STATUS.FAILED}; two plugins name this band`)
  })

  it('gives the dot to an unavailable dependency, and keeps the clash in the description', () => {
    const mark = navMark({
      status: PLUGIN_STATUS.ACTIVE,
      health: health(HEALTH_STATE.UNAVAILABLE),
      clashes: [clash('two plugins name this band')],
    })

    expect(mark.tone).toBe('warn')
    expect(mark.title).toBe('a dependency is unavailable')
    expect(mark.description).toBe('a dependency is unavailable; two plugins name this band')
  })

  it('never says the same thing twice when the clash IS the mark', () => {
    const mark = navMark({ clashes: [clash('said once')] })

    expect(mark.description).toBe('said once')
  })

  it('still returns ONE mark — the nav item has room for one dot', () => {
    const mark = navMark({
      status: PLUGIN_STATUS.FAILED,
      health: health(HEALTH_STATE.UNAVAILABLE),
      clashes: [clash('and a clash as well')],
    })

    expect(Object.keys(mark).sort()).toEqual(['description', 'title', 'tone'])
    expect(mark.tone).toBe('err')
  })
})
