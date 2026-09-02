/*
  The placement rules, read straight off the plan (BR-AS02, AS04, AS06, AS07,
  AS56, AS58).

  Nothing here builds a registry, and nothing here is reactive. A rule that
  used to need "index a registry, then read seven containers back" is now one
  call and one assertion about a plain object — including the two rules that
  were previously only observable as an absence.
*/
import { describe, expect, it, vi } from 'vitest'

import { validateManifest } from '../registry/manifestSchema.js'
import { PLUGIN_STATUS } from '../registry/pluginStatus.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { decidePlacements } from './placementPolicy.js'

const plugin = (id, contributions, overrides = {}) => {
  const result = validateManifest({
    id,
    name: id,
    schemaVersion: REGISTRY_SCHEMA_VERSION,
    shellApiVersion: SHELL_API_VERSION,
    remote: { kind: 'federated', url: `http://localhost:7110/${id}.js`, module: './plugin' },
    contributions,
    ...overrides,
  })
  if (!result.ok) throw new Error(`fixture is invalid: ${result.message}`)
  return result.plugin
}

/* An empty shell that permits everything and never runs out of room. Each
   spec overrides only the question its rule is about. */
const context = (overrides = {}) => ({
  alreadyIndexed: () => false,
  permits: () => true,
  prefixOwner: () => undefined,
  occupancy: () => 0,
  accepts: () => ({ ok: true }),
  ownerWithdrawn: () => false,
  routePlaced: () => false,
  ...overrides,
})

const decide = (plugins, overrides) => decidePlacements(plugins, context(overrides))

const opsOf = (plan) => plan.actions.map((a) => `${a.op}:${a.contribution.qualifiedId}`)
const codesOf = (plan) => plan.refusals.map((r) => r.code)
const statusOf = (plan, pluginId) => plan.statuses.find((s) => s.pluginId === pluginId)

describe('deciding is not doing', () => {
  it('returns a plan and touches nothing it was asked about', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
      { kind: 'navigation', id: 'vessels-nav', label: 'Vessels', route: 'vessels' },
    ])
    const accepts = vi.fn(() => ({ ok: true }))

    const plan = decide([p], { accepts })

    expect(opsOf(plan)).toEqual(['route:fleet-ops/vessels', 'navigation:fleet-ops/vessels-nav'])
    expect(plan.indexed.map((x) => x.id)).toEqual(['fleet-ops'])
    // Neither a route nor a nav entry lands in a slot, so nothing asked.
    expect(accepts).not.toHaveBeenCalled()
  })

  it('skips a plugin the shell has already ruled on', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
    ])

    const plan = decide([p], { alreadyIndexed: () => true })

    expect(plan.indexed).toEqual([])
    expect(plan.actions).toEqual([])
    expect(plan.statuses).toEqual([])
  })

  it('places nothing for a plugin the operator switched off', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
    ], { enabled: false })

    const plan = decide([p])

    expect(plan.actions).toEqual([])
    expect(statusOf(plan, 'fleet-ops')).toMatchObject({
      to: PLUGIN_STATUS.DISABLED,
      code: 'operator-disabled',
    })
  })
})

describe('BR-AS02 — one route prefix, one owner', () => {
  it('gives the prefix to the first claim and refuses the second plugin`s route', () => {
    const a = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
    ])
    const b = plugin('impostor', [
      { kind: 'route', id: 'sneaky', path: '/fleet-ops/sneaky', title: 'Sneaky' },
      { kind: 'shell-footer', id: 'mine', label: 'Mine' },
    ], { routePrefix: 'fleet-ops' })

    const plan = decide([a, b])

    expect(opsOf(plan)).toEqual(['route:fleet-ops/vessels', 'shell-footer:impostor/mine'])
    expect(codesOf(plan)).toEqual(['route-prefix-conflict'])
    // The loser keeps everything of its own that is not a route.
    expect(statusOf(plan, 'impostor').to).toBe(PLUGIN_STATUS.AVAILABLE)
  })

  it('refuses against a prefix an earlier pass already claimed', () => {
    const b = plugin('impostor', [
      { kind: 'route', id: 'sneaky', path: '/fleet-ops/sneaky', title: 'Sneaky' },
    ], { routePrefix: 'fleet-ops' })

    const plan = decide([b], { prefixOwner: () => 'fleet-ops' })

    expect(codesOf(plan)).toEqual(['route-prefix-conflict'])
    expect(plan.prefixClaims).toEqual([])
  })

  it('lets a plugin re-claim the prefix it already owns', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
    ])

    const plan = decide([p], { prefixOwner: () => 'fleet-ops' })

    expect(codesOf(plan)).toEqual([])
    expect(opsOf(plan)).toEqual(['route:fleet-ops/vessels'])
  })
})

describe('BR-AS04 — a refusal costs one contribution, never a sibling', () => {
  it('refuses only what the session may not see', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
      { kind: 'shell-footer', id: 'secret', label: 'Secret', permission: 'fleet:admin' },
    ])

    const plan = decide([p], { permits: (permission) => permission !== 'fleet:admin' })

    expect(opsOf(plan)).toEqual(['route:fleet-ops/vessels'])
    expect(codesOf(plan)).toEqual(['permission-denied'])
    expect(statusOf(plan, 'fleet-ops').to).toBe(PLUGIN_STATUS.AVAILABLE)
  })

  it('disables a plugin whose every contribution was withheld, and says why', () => {
    const p = plugin('fleet-ops', [
      { kind: 'shell-footer', id: 'secret', label: 'Secret', permission: 'fleet:admin' },
    ])

    const plan = decide([p], { permits: () => false })

    expect(statusOf(plan, 'fleet-ops')).toMatchObject({
      to: PLUGIN_STATUS.DISABLED,
      code: 'no-permitted-contributions',
    })
  })

  it('never asks a slot for room for a contribution the session may not see', () => {
    /* A withheld contribution that took up capacity would cost a slot to
       someone who could have used it. */
    const accepts = vi.fn(() => ({ ok: true }))
    const p = plugin('fleet-ops', [
      { kind: 'shell-footer', id: 'secret', label: 'Secret', permission: 'fleet:admin' },
    ])

    decide([p], { permits: () => false, accepts })

    expect(accepts).not.toHaveBeenCalled()
  })
})

describe('BR-AS07 — a slot has room, and the plan counts its own placements', () => {
  it('counts what the shell already holds plus what this plan added', () => {
    const seen = []
    const p = plugin('fleet-ops', [
      { kind: 'shell-footer', id: 'a', label: 'A' },
      { kind: 'shell-footer', id: 'b', label: 'B' },
    ])

    decide([p], {
      occupancy: () => 2,
      accepts: (pointId, info) => {
        seen.push(info.placedCount)
        return { ok: true }
      },
    })

    expect(seen).toEqual([2, 3])
  })

  it('refuses with the slot`s own verdict once it is full', () => {
    const p = plugin('fleet-ops', [
      { kind: 'shell-footer', id: 'a', label: 'A' },
      { kind: 'shell-footer', id: 'b', label: 'B' },
    ])

    const plan = decide([p], {
      accepts: (pointId, { placedCount }) =>
        placedCount === 0 ? { ok: true } : { ok: false, code: 'point-full', message: 'no room' },
    })

    expect(opsOf(plan)).toEqual(['shell-footer:fleet-ops/a'])
    expect(plan.refusals).toEqual([
      expect.objectContaining({ code: 'point-full', message: 'no room' }),
    ])
  })
})

describe('BR-AS58 — a placement into an absent slot is held, not refused', () => {
  it('suspends it and records no refusal', () => {
    const p = plugin('fleet-ops', [
      { kind: 'extension', id: 'panel', target: 'depot/side/v1', component: './Panel' },
    ])

    const plan = decide([p], { ownerWithdrawn: (owner) => owner === 'depot' })

    expect(plan.refusals).toEqual([])
    expect(plan.suspensions).toEqual([
      { pointId: 'depot/side/v1', contribution: expect.objectContaining({ qualifiedId: 'fleet-ops/panel' }) },
    ])
    // Held is not placed, so this plugin has nothing on screen.
    expect(statusOf(plan, 'fleet-ops').code).toBe('no-permitted-contributions')
  })
})

describe('BR-AS06 — a nav entry must name a route that exists', () => {
  it('resolves against a route declared later in the same manifest', () => {
    const p = plugin('fleet-ops', [
      { kind: 'navigation', id: 'vessels-nav', label: 'Vessels', route: 'vessels' },
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
    ])

    const plan = decide([p])

    expect(plan.dropNavigation).toEqual([])
    expect(codesOf(plan)).toEqual([])
  })

  it('resolves against a route an earlier pass placed', () => {
    const p = plugin('fleet-ops', [
      { kind: 'navigation', id: 'vessels-nav', label: 'Vessels', route: 'vessels' },
    ])

    const plan = decide([p], { routePlaced: (id) => id === 'fleet-ops/vessels' })

    expect(plan.dropNavigation).toEqual([])
  })

  it('drops one that names a route nobody placed, and says so', () => {
    const p = plugin('fleet-ops', [
      { kind: 'navigation', id: 'vessels-nav', label: 'Vessels', route: 'vessels' },
    ])

    const plan = decide([p])

    expect(plan.dropNavigation).toEqual(['fleet-ops/vessels-nav'])
    expect(codesOf(plan)).toEqual(['unresolved-route'])
  })

  it('still calls the plugin available — the shell placed it and took it back', () => {
    const p = plugin('fleet-ops', [
      { kind: 'navigation', id: 'vessels-nav', label: 'Vessels', route: 'vessels' },
    ])

    expect(statusOf(decide([p]), 'fleet-ops').to).toBe(PLUGIN_STATUS.AVAILABLE)
  })
})

describe('every contribution stays known, placed or not', () => {
  it('lists the refused ones too, so a screen can explain them', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
      { kind: 'shell-footer', id: 'secret', label: 'Secret', permission: 'fleet:admin' },
    ])

    const plan = decide([p], { permits: (permission) => permission !== 'fleet:admin' })

    expect(plan.known.map((c) => c.qualifiedId)).toEqual(['fleet-ops/vessels', 'fleet-ops/secret'])
  })
})
