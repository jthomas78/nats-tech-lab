import { describe, expect, it } from 'vitest'

import { declareShellExtensionPoints } from '../extensions/extensionPoints.js'
import { validateManifest } from '../registry/manifestSchema.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { createContributionRegistry } from './contributionRegistry.js'

/*
  A withdrawn plugin that owns a slot (BR-AS58).

  The contributors into that slot did nothing wrong, and they are usually
  other plugins entirely. So the withdrawal suspends the PLACEMENTS pointing
  at the missing slot and leaves the contributors themselves running
  everywhere else — and when the slot comes back unchanged, those placements
  come back exactly once.
*/

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

const SIDEBAR = 'catalog/sidebar/v1'

const owner = () =>
  plugin(
    'catalog',
    [{ kind: 'route', id: 'home', path: '/catalog', title: 'Catalog' }],
    { extensionPoints: [{ id: SIDEBAR }] },
  )

const contributor = (id) =>
  plugin(id, [
    { kind: 'extension', id: 'panel', target: SIDEBAR, component: './Panel' },
    { kind: 'extension', id: 'card', target: 'shell/home-main/v1', component: './Card' },
  ])

const build = () => {
  const extensionPoints = declareShellExtensionPoints()
  extensionPoints.declare({ id: SIDEBAR })
  return createContributionRegistry({ extensionPoints, permissions: { can: () => true } })
}

describe('BR-AS58 — withdrawing a slot owner suspends the placements, not the contributors', () => {
  it('empties the withdrawn slot and leaves the contributor everywhere else', () => {
    const registry = build().index([owner(), contributor('billing')])

    registry.withdraw('catalog')

    expect(registry.extensionsFor(SIDEBAR)).toEqual([])
    expect(registry.extensionsFor('shell/home-main/v1').map((c) => c.qualifiedId)).toEqual([
      'billing/card',
    ])
    expect(registry.isWithdrawn('billing')).toBe(false)
  })

  it('records no refusal, because the contributor is not at fault', () => {
    const registry = build().index([owner(), contributor('billing')])

    registry.withdraw('catalog')

    expect(registry.refusals.filter((r) => r.pluginId === 'billing')).toEqual([])
  })

  it('leaves host-owned slots alone', () => {
    const registry = build().index([owner(), contributor('billing')])

    registry.withdraw('catalog')

    expect(registry.extensionsFor('shell/home-main/v1')).toHaveLength(1)
  })
})

describe('BR-AS58 — the slot returns, and the placements come back once', () => {
  it('restores every suspended placement exactly once', () => {
    const registry = build().index([owner(), contributor('billing'), contributor('pricing')])

    registry.withdraw('catalog')
    registry.restore('catalog')

    expect(registry.extensionsFor(SIDEBAR).map((c) => c.qualifiedId)).toEqual([
      'billing/panel',
      'pricing/panel',
    ])
  })

  it('does not restore a contributor that has itself been withdrawn meanwhile', () => {
    const registry = build().index([owner(), contributor('billing'), contributor('pricing')])

    registry.withdraw('catalog')
    registry.withdraw('billing')
    registry.restore('catalog')

    expect(registry.extensionsFor(SIDEBAR).map((c) => c.qualifiedId)).toEqual(['pricing/panel'])
  })

  it('puts that contributor back into the slot when it returns too', () => {
    const registry = build().index([owner(), contributor('billing')])

    registry.withdraw('catalog')
    registry.withdraw('billing')
    registry.restore('catalog')
    registry.restore('billing')

    expect(registry.extensionsFor(SIDEBAR).map((c) => c.qualifiedId)).toEqual(['billing/panel'])
  })

  it('keeps a contribution out of a slot whose owner is still withdrawn', () => {
    const registry = build().index([owner(), contributor('billing')])

    registry.withdraw('catalog')
    registry.withdraw('billing')
    registry.restore('billing')

    // Its other placements come back; the one aimed at the missing slot does
    // not, and it is suspended rather than refused.
    expect(registry.extensionsFor('shell/home-main/v1')).toHaveLength(1)
    expect(registry.extensionsFor(SIDEBAR)).toEqual([])
    expect(registry.refusals.filter((r) => r.pluginId === 'billing')).toEqual([])
  })
})
