import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'

import { RELOAD_REASON } from '../registry/registryDiff.js'
import { SHELL } from '../shellKey.js'
import RegistrySignalBanner from './RegistrySignalBanner.vue'

const mountWith = (pendingReload, registry = { revision: '50' }) =>
  mount(RegistrySignalBanner, {
    global: { provide: { [SHELL]: reactive({ pendingReload, registry }) } },
  })

describe('BR-AS19 — a registry change notifies, and never unloads', () => {
  it('says nothing while nothing needs a reload', () => {
    expect(mountWith([]).find('[data-testid="registry-signal"]').exists()).toBe(false)
  })

  it('offers a reload for a withdrawn entry and names it', () => {
    const w = mountWith([{ id: 'fleet-ops', name: 'Fleet Ops', reason: RELOAD_REASON.REMOVED }])
    expect(w.find('[data-testid="registry-signal-summary"]').text()).toContain('Fleet Ops')
    expect(w.find('[data-testid="registry-signal-reload"]').exists()).toBe(true)
  })

  it('offers, and does not apply — there is no affordance that unloads a plugin', () => {
    // Decision 25 in one assertion: the only verbs on this bar are "reload"
    // and "not now". An "unload"/"disable"/"remove" button here would tear a
    // mounted plugin down under the user.
    const w = mountWith([{ id: 'fleet-ops', name: 'Fleet Ops', reason: RELOAD_REASON.REMOVED }])
    const text = w.text().toLowerCase()
    for (const verb of ['unload', 'disable', 'remove', 'stop']) expect(text).not.toContain(verb)
  })

  it('can be dismissed without applying anything', async () => {
    const w = mountWith([{ id: 'fleet-ops', name: 'Fleet Ops', reason: RELOAD_REASON.REMOVED }])
    await w.find('[data-testid="registry-signal-dismiss"]').trigger('click')
    expect(w.find('[data-testid="registry-signal"]').exists()).toBe(false)
  })

  it('reports a moved remote as a build change, not as a removal', () => {
    const w = mountWith([{ id: 'fleet-ops', name: 'Fleet Ops', reason: RELOAD_REASON.REMOTE_CHANGED }])
    const text = w.find('[data-testid="registry-signal-summary"]').text()
    expect(text).toContain('different build')
    expect(text).not.toContain('withdrawn')
  })

  /* Decision 46 made an edit the common reason to raise this banner, so the
     copy for it is a rule and not a nicety: without a clause the banner said
     "The plugin catalog changed. . Still running until you reload." */
  it('names an edited entry rather than announcing a change and naming nothing', () => {
    const w = mountWith([{ id: 'fleet-ops', name: 'Fleet Ops', reason: RELOAD_REASON.CHANGED }])
    const text = w.find('[data-testid="registry-signal-summary"]').text()
    expect(text).toContain('Fleet Ops')
    expect(text).toContain('edited')
    expect(text).not.toContain('withdrawn')
  })

  it('names each reason separately when one read produced several', () => {
    const w = mountWith([
      { id: 'fleet-ops', name: 'Fleet Ops', reason: RELOAD_REASON.CHANGED },
      { id: 'billing', name: 'Billing', reason: RELOAD_REASON.REMOVED },
    ])
    const text = w.find('[data-testid="registry-signal-summary"]').text()
    expect(text).toContain('Fleet Ops edited')
    expect(text).toContain('Billing withdrawn')
  })

  it('names the revision but never the registry endpoint (BR-AS04)', () => {
    const w = mountWith([{ id: 'fleet-ops', name: 'Fleet Ops', reason: RELOAD_REASON.REMOVED }])
    expect(w.find('[data-testid="registry-signal-revision"]').text()).toContain('50')
    expect(w.text()).not.toContain('/api/')
    expect(w.text()).not.toContain('http')
  })
})

/*
  BR-AS49 / decision 100 — a revocation is taken, not offered.

  Every other reason on this bar waits for the user, because tearing a mounted
  plugin down under them is worse than showing it a little longer. A revoked
  publisher key inverts that: the code on screen is code the platform has
  withdrawn trust from, and waiting is the worse option.

  What this promises is exactly what decision 100 promises and no more — the
  plugin stops at the next paint. It does not interrupt an in-flight callback,
  and it is not isolation.
*/
describe('BR-AS49 — a revoked entry reloads rather than asking', () => {
  const revoked = { id: 'fleet-ops', name: 'Fleet Ops', reason: RELOAD_REASON.REVOKED, forced: true }

  const withStubbedReload = (fn) => {
    const original = globalThis.location
    const calls = []
    Object.defineProperty(globalThis, 'location', {
      configurable: true,
      value: { reload: () => calls.push('reload') },
    })
    try {
      return fn(calls)
    } finally {
      Object.defineProperty(globalThis, 'location', { configurable: true, value: original })
    }
  }

  it('reloads on its own, without waiting to be clicked', () =>
    withStubbedReload((calls) => {
      mountWith([revoked])
      expect(calls).toEqual(['reload'])
    }))

  it('does not reload for an ordinary catalog change', () =>
    withStubbedReload((calls) => {
      mountWith([{ id: 'fleet-ops', name: 'Fleet Ops', reason: RELOAD_REASON.REMOVED }])
      expect(calls).toEqual([])
    }))

  it('says the plugin was withdrawn by the platform, not that it is still running', () =>
    withStubbedReload(() => {
      const w = mountWith([revoked])
      const text = w.find('[data-testid="registry-signal-summary"]').text()
      expect(text).toContain('Fleet Ops')
      expect(text).toContain('withdrawn')
      expect(text).not.toContain('Still running until you reload')
    }))

  it('cannot be dismissed, because dismissing it would not stop anything', () =>
    withStubbedReload(async () => {
      const w = mountWith([revoked])
      expect(w.find('[data-testid="registry-signal-dismiss"]').exists()).toBe(false)
    }))
})

/*
  BR-AS51 / decision 105 — a degraded read says how stale it is.
*/
describe('BR-AS51 — a degraded read is labelled', () => {
  it('shows the revision the stale copy was taken at', () => {
    const w = mountWith([{ id: 'fleet-ops', name: 'Fleet Ops', reason: RELOAD_REASON.REMOVED }], {
      revision: '50',
      degraded: true,
    })
    expect(w.find('[data-testid="registry-signal-degraded"]').text()).toContain('degraded')
    expect(w.find('[data-testid="registry-signal-degraded"]').text()).toContain('50')
  })

  it('says nothing about staleness on a healthy read', () => {
    const w = mountWith([{ id: 'fleet-ops', name: 'Fleet Ops', reason: RELOAD_REASON.REMOVED }])
    expect(w.find('[data-testid="registry-signal-degraded"]').exists()).toBe(false)
  })
})
