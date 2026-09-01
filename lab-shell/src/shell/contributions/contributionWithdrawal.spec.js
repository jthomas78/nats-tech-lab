import { describe, expect, it } from 'vitest'

import { declareShellExtensionPoints } from '../extensions/extensionPoints.js'
import { validateManifest } from '../registry/manifestSchema.js'
import { PLUGIN_STATUS, PluginStatusRecord } from '../registry/pluginStatus.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { createContributionRegistry } from './contributionRegistry.js'

/*
  What a running shell does when a publisher says a plugin is gone (BR-AS56)
  and when it comes back unchanged (BR-AS59). The unit under test is placement
  only: nothing here loads code, and nothing here decides — the registry is
  told to withdraw and told to restore.
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

const everything = { can: () => true }

const build = (permissions = everything) =>
  createContributionRegistry({ extensionPoints: declareShellExtensionPoints(), permissions })

const fullPlugin = (id) =>
  plugin(id, [
    { kind: 'route', id: 'home', path: `/${id}/home`, title: 'Home' },
    { kind: 'navigation', id: 'home-nav', label: 'Home', route: 'home' },
    { kind: 'shell-footer', id: 'foot' },
  ])

describe('BR-AS56 — withdrawal removes what the plugin contributed', () => {
  it('takes away its routes, navigation and footer items', () => {
    const registry = build().index([fullPlugin('fleet-ops')])

    registry.withdraw('fleet-ops')

    expect(registry.routes).toEqual([])
    expect(registry.navigation).toEqual([])
    expect(registry.shellFooter).toEqual([])
    expect(registry.all).toEqual([])
    expect(registry.isWithdrawn('fleet-ops')).toBe(true)
  })

  it('leaves every sibling alone, including at a shared extension point', () => {
    const registry = build().index([fullPlugin('fleet-ops'), fullPlugin('pricing')])

    registry.withdraw('fleet-ops')

    expect(registry.routes.map((r) => r.pluginId)).toEqual(['pricing'])
    expect(registry.navigation.map((n) => n.pluginId)).toEqual(['pricing'])
    expect(registry.shellFooter.map((c) => c.pluginId)).toEqual(['pricing'])
    expect(registry.isWithdrawn('pricing')).toBe(false)
  })

  it('is safe to repeat, because a duplicate event is ordinary', () => {
    const registry = build().index([fullPlugin('fleet-ops'), fullPlugin('pricing')])

    registry.withdraw('fleet-ops')
    registry.withdraw('fleet-ops')

    expect(registry.routes.map((r) => r.pluginId)).toEqual(['pricing'])
  })

  it('says nothing about a plugin it never placed', () => {
    const registry = build().index([fullPlugin('fleet-ops')])

    expect(registry.withdraw('never-heard-of-it')).toBe(false)
    expect(registry.routes).toHaveLength(1)
  })

  it('marks the plugin withdrawn rather than disabled', () => {
    const p = fullPlugin('fleet-ops')
    const statuses = new Map([[p.id, new PluginStatusRecord(p.id, { name: p.name })]])
    const registry = build().index([p], statuses)

    registry.withdraw('fleet-ops', statuses)

    expect(statuses.get('fleet-ops').status).toBe(PLUGIN_STATUS.WITHDRAWN)
  })

  it('cannot be resurrected by a later re-index of the same document', () => {
    const p = fullPlugin('fleet-ops')
    const registry = build().index([p])

    registry.withdraw('fleet-ops')
    // The shell re-reads the registry and re-indexes everything it holds; an
    // import or activation finishing late does the same thing by another
    // route. Neither may put a withdrawn plugin back on screen.
    registry.index([p])

    expect(registry.routes).toEqual([])
  })
})

describe('BR-AS59 — an unchanged return puts them back, once', () => {
  it('restores every kind, without duplicating any of them', () => {
    const registry = build().index([fullPlugin('fleet-ops')])
    const before = registry.all.map((c) => c.qualifiedId)

    registry.withdraw('fleet-ops')
    expect(registry.restore('fleet-ops')).toBe(true)

    expect(registry.routes).toHaveLength(1)
    expect(registry.navigation).toHaveLength(1)
    expect(registry.shellFooter).toHaveLength(1)
    expect(registry.all.map((c) => c.qualifiedId)).toEqual(before)
    expect(registry.isWithdrawn('fleet-ops')).toBe(false)
  })

  it('restores in the shell-wide order, not at the end', () => {
    const first = plugin('alpha', [{ kind: 'shell-footer', id: 'a', order: 1 }])
    const second = plugin('zulu', [{ kind: 'shell-footer', id: 'z', order: 2 }])
    const registry = build().index([first, second])

    registry.withdraw('alpha')
    registry.restore('alpha')

    expect(registry.shellFooter.map((c) => c.qualifiedId)).toEqual(['alpha/a', 'zulu/z'])
  })

  it('is a no-op for a plugin that is not withdrawn', () => {
    const registry = build().index([fullPlugin('fleet-ops')])

    expect(registry.restore('fleet-ops')).toBe(false)
    expect(registry.routes).toHaveLength(1)
  })

  it('re-checks permission, so an ineligible placement stays absent', () => {
    // The session's claims can change between withdrawal and return; a return
    // is a fresh placement decision, never a replay of the old one.
    let allowed = true
    const registry = build({ can: () => allowed }).index([fullPlugin('fleet-ops')])

    registry.withdraw('fleet-ops')
    allowed = false
    registry.restore('fleet-ops')

    expect(registry.routes).toEqual([])
    expect(registry.refusals.map((r) => r.code)).toContain('permission-denied')
  })
})
