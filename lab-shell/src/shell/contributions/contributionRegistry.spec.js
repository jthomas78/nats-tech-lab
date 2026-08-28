import { describe, expect, it } from 'vitest'

import { createPermissionEvaluator } from '../auth/permissions.js'
import { declareShellExtensionPoints } from '../extensions/extensionPoints.js'
import { validateManifest } from '../registry/manifestSchema.js'
import { PLUGIN_STATUS, PluginStatusRecord } from '../registry/pluginStatus.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { createContributionRegistry } from './contributionRegistry.js'

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

const statusesFor = (...plugins) =>
  new Map(plugins.map((p) => [p.id, new PluginStatusRecord(p.id, { name: p.name })]))

const build = ({ permissions = createPermissionEvaluator({ permissions: ['*'] }), extensionPoints } = {}) =>
  createContributionRegistry({
    extensionPoints: extensionPoints ?? declareShellExtensionPoints(),
    permissions,
  })

describe('BR-AS08 — indexing places contributions without loading code', () => {
  it('produces routes and nav entries from metadata alone', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
      { kind: 'navigation', id: 'vessels-nav', label: 'Vessels', route: 'vessels' },
    ])
    const registry = build().index([p])

    expect(registry.routes.map((r) => r.path)).toEqual(['/fleet-ops/vessels'])
    expect(registry.navigation.map((n) => n.label)).toEqual(['Vessels'])
  })

  it('marks a plugin available, not active, once indexed', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
    ])
    const statuses = statusesFor(p)

    build().index([p], statuses)

    expect(statuses.get('fleet-ops').status).toBe(PLUGIN_STATUS.AVAILABLE)
  })
})

describe('BR-AS06 — deterministic order across plugins', () => {
  it('orders by order, then plugin id, then declaration index', () => {
    const a = plugin('zulu-plugin', [
      { kind: 'shell-footer', id: 'z1', order: 1 },
      { kind: 'shell-footer', id: 'z2', order: 1 },
    ])
    const b = plugin('alpha-plugin', [{ kind: 'shell-footer', id: 'a1', order: 1 }])

    const forwards = build().index([a, b]).shellFooter.map((c) => c.qualifiedId)
    const backwards = build().index([b, a]).shellFooter.map((c) => c.qualifiedId)

    expect(forwards).toEqual(['alpha-plugin/a1', 'zulu-plugin/z1', 'zulu-plugin/z2'])
    // The order plugins arrive from the registry must not change the UI.
    expect(backwards).toEqual(forwards)
  })

  it('honours an explicit order across plugin boundaries', () => {
    const a = plugin('alpha-plugin', [{ kind: 'shell-footer', id: 'late', order: 90 }])
    const b = plugin('zulu-plugin', [{ kind: 'shell-footer', id: 'early', order: 10 }])

    expect(build().index([a, b]).shellFooter.map((c) => c.id)).toEqual(['early', 'late'])
  })
})

describe('BR-AS05 — permissions decide placement', () => {
  it('leaves an unpermitted contribution out of the nav', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
      { kind: 'navigation', id: 'nav', label: 'Vessels', route: 'vessels', permission: 'fleet-ops.admin' },
    ])
    const registry = build({ permissions: createPermissionEvaluator({ permissions: [] }) }).index([p])

    expect(registry.navigation).toHaveLength(0)
    expect(registry.refusals[0].code).toBe('permission-denied')
  })

  it('places the permitted contributions of a plugin whose others are refused', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
      { kind: 'shell-footer', id: 'secret', permission: 'fleet-ops.admin' },
    ])
    const registry = build({
      permissions: createPermissionEvaluator({ permissions: ['fleet-ops.vessels.read'] }),
    }).index([p])

    expect(registry.routes).toHaveLength(1)
    expect(registry.shellFooter).toHaveLength(0)
  })

  it('does not let an unpermitted contribution consume extension-point capacity', () => {
    // Otherwise a contribution nobody can see would push out one they can.
    const hidden = plugin('hidden-plugin', [
      { kind: 'extension', id: 'panel', target: 'shell/home-main/v1', permission: 'nope.at.all' },
    ])
    const visible = plugin('visible-plugin', [
      { kind: 'extension', id: 'panel', target: 'shell/home-main/v1' },
    ])
    const points = declareShellExtensionPoints()
    const registry = createContributionRegistry({
      extensionPoints: points,
      permissions: createPermissionEvaluator({ permissions: ['visible-plugin.*'] }),
    }).index([hidden, visible])

    expect(registry.extensionsFor('shell/home-main/v1').map((c) => c.pluginId)).toEqual([
      'visible-plugin',
    ])
  })

  it('disables a plugin whose every contribution is refused', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'V', permission: 'nope' },
    ])
    const statuses = statusesFor(p)

    build({ permissions: createPermissionEvaluator({ permissions: [] }) }).index([p], statuses)

    expect(statuses.get('fleet-ops').status).toBe(PLUGIN_STATUS.DISABLED)
    expect(statuses.get('fleet-ops').reasonCode).toBe('no-permitted-contributions')
  })
})

describe('BR-AS03 — an operator disable takes effect without a shell change', () => {
  it('indexes nothing from a disabled plugin', () => {
    const p = plugin(
      'fleet-ops',
      [{ kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' }],
      { enabled: false },
    )
    const statuses = statusesFor(p)
    const registry = build().index([p], statuses)

    expect(registry.routes).toHaveLength(0)
    expect(statuses.get('fleet-ops').reasonCode).toBe('operator-disabled')
  })
})

describe('BR-AS07 — the host owns capacity and versioning', () => {
  it('refuses the contribution that exceeds a point capacity, keeping the earlier ones', () => {
    const points = declareShellExtensionPoints()
    points.declare({ id: 'demo-catalog/details-sidebar/v1', capacity: 1 })
    const a = plugin('alpha-plugin', [
      { kind: 'extension', id: 'p', target: 'demo-catalog/details-sidebar/v1' },
    ])
    const b = plugin('bravo-plugin', [
      { kind: 'extension', id: 'p', target: 'demo-catalog/details-sidebar/v1' },
    ])
    const registry = build({ extensionPoints: points }).index([a, b])

    expect(registry.extensionsFor('demo-catalog/details-sidebar/v1')).toHaveLength(1)
    expect(registry.refusals.map((r) => r.code)).toEqual(['extension-point-full'])
  })

  it('refuses an extension targeting a point nobody declares', () => {
    const p = plugin('fleet-ops', [
      { kind: 'extension', id: 'p', target: 'shell/nowhere/v1' },
    ])
    const registry = build().index([p])

    expect(registry.refusals[0].code).toBe('unknown-extension-point')
  })

  it('refuses an extension built against an older major of a live point', () => {
    const p = plugin('fleet-ops', [{ kind: 'extension', id: 'p', target: 'shell/home-main/v9' }])

    expect(build().index([p]).refusals[0].code).toBe('unsupported-extension-point-version')
  })
})

describe('route-scoped shell controls', () => {
  const controls = () =>
    build().index([
      plugin('fleet-ops', [
        { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
        {
          kind: 'shell-control',
          id: 'refresh',
          region: 'shell/topbar-controls/v1',
          routes: ['/fleet-ops'],
        },
        { kind: 'shell-control', id: 'always', region: 'shell/topbar-controls/v1' },
      ]),
    ])

  it('shows a scoped control only under its route prefix', () => {
    expect(controls().shellControlsFor('/fleet-ops/vessels').map((c) => c.id)).toEqual([
      'refresh',
      'always',
    ])
    expect(controls().shellControlsFor('/demos').map((c) => c.id)).toEqual(['always'])
  })

  it('does not treat a sibling path as inside the scope', () => {
    // '/fleet-ops-archive' shares a string prefix with '/fleet-ops' but is a
    // different route.
    expect(controls().shellControlsFor('/fleet-ops-archive').map((c) => c.id)).toEqual(['always'])
  })
})

describe('BR-AS04 — one bad contribution costs only itself', () => {
  it('keeps a plugin route when its footer item is refused', () => {
    const points = declareShellExtensionPoints()
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
      { kind: 'extension', id: 'broken', target: 'shell/nowhere/v1' },
    ])
    const statuses = statusesFor(p)
    const registry = build({ extensionPoints: points }).index([p], statuses)

    expect(registry.routes).toHaveLength(1)
    expect(statuses.get('fleet-ops').status).toBe(PLUGIN_STATUS.AVAILABLE)
  })

  it('drops a nav entry whose route was never placed, without dropping the plugin', () => {
    const p = plugin('fleet-ops', [
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'V', permission: 'nope' },
      { kind: 'navigation', id: 'nav', label: 'Vessels', route: 'vessels' },
      { kind: 'shell-footer', id: 'status' },
    ])
    const registry = build({
      permissions: createPermissionEvaluator({ permissions: ['fleet-ops.status'] }),
    }).index([p])

    expect(registry.navigation).toHaveLength(0)
    expect(registry.refusals.map((r) => r.code)).toContain('unresolved-route')
    expect(registry.shellFooter).toHaveLength(1)
  })

  it('resolves a nav entry that names a route declared after it', () => {
    const p = plugin('fleet-ops', [
      { kind: 'navigation', id: 'nav', label: 'Vessels', route: 'vessels' },
      { kind: 'route', id: 'vessels', path: '/fleet-ops/vessels', title: 'Vessels' },
    ])

    expect(build().index([p]).navigation).toHaveLength(1)
  })
})

describe('BR-AS12 — one route prefix, one owner', () => {
  it('lets a plugin serve a path segment that is not its id', () => {
    const p = plugin('demo-catalog', [
      { kind: 'route', id: 'catalog', path: '/demos', title: 'Demos' },
    ], { routePrefix: 'demos' })

    expect(build().index([p]).routes.map((r) => r.path)).toEqual(['/demos'])
  })

  it('refuses the second plugin to claim a prefix, keeping the first', () => {
    const first = plugin('demo-catalog', [
      { kind: 'route', id: 'catalog', path: '/demos', title: 'Demos' },
    ], { routePrefix: 'demos' })
    const second = plugin('impostor-plugin', [
      { kind: 'route', id: 'catalog', path: '/demos', title: 'Demos' },
      { kind: 'shell-footer', id: 'status' },
    ], { routePrefix: 'demos' })

    const registry = build().index([first, second])

    expect(registry.routes.map((r) => r.pluginId)).toEqual(['demo-catalog'])
    expect(registry.refusals.map((r) => r.code)).toContain('route-prefix-conflict')
    // The conflict costs the impostor its routes, not its footer item.
    expect(registry.shellFooter).toHaveLength(1)
  })
})
