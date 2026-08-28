import { describe, expect, it } from 'vitest'

import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { parseExtensionPointId, validateManifest, validateRegistryDocument } from './manifestSchema.js'

const manifest = (overrides = {}) => ({
  id: 'example-plugin',
  name: 'Example Plugin',
  schemaVersion: REGISTRY_SCHEMA_VERSION,
  shellApiVersion: SHELL_API_VERSION,
  remote: { kind: 'federated', url: 'http://localhost:7110/remoteEntry.js', module: './plugin' },
  contributions: [
    { kind: 'route', id: 'vessels', path: '/example-plugin/vessels', title: 'Vessels' },
  ],
  ...overrides,
})

describe('BR-AS13 — contract compatibility', () => {
  it('rejects a manifest whose schemaVersion this shell does not support', () => {
    const result = validateManifest(manifest({ schemaVersion: REGISTRY_SCHEMA_VERSION + 1 }))

    expect(result.ok).toBe(false)
    expect(result.code).toBe('unsupported-schema-version')
  })

  it('rejects a manifest built against a different shell API version', () => {
    const result = validateManifest(manifest({ shellApiVersion: SHELL_API_VERSION + 1 }))

    expect(result.ok).toBe(false)
    expect(result.code).toBe('unsupported-shell-api-version')
  })

  it('reports the version mismatch rather than the shape it could not read', () => {
    // An incompatible plugin is entitled to a manifest shape this shell does
    // not understand. Complaining about its contributions would name a
    // consequence instead of the cause, and would send whoever reads the
    // Plugins screen after the wrong thing.
    const result = validateManifest(
      manifest({ schemaVersion: 99, contributions: [{ kind: 'wormhole', id: 'x' }] }),
    )

    expect(result.code).toBe('unsupported-schema-version')
  })

  it('rejects an unknown contribution kind', () => {
    const result = validateManifest(
      manifest({ contributions: [{ kind: 'sidebar-takeover', id: 'x' }] }),
    )

    expect(result.ok).toBe(false)
    expect(result.code).toBe('unknown-contribution-kind')
  })

  it('rejects a registry document whose own schemaVersion is unsupported', () => {
    const result = validateRegistryDocument({ schemaVersion: 0, plugins: [] })

    expect(result.ok).toBe(false)
    expect(result.code).toBe('unsupported-schema-version')
  })

  it('never reads remote code to decide compatibility', () => {
    // The rejection is reachable from metadata alone: this manifest names a
    // remote that could not possibly load, and validation still answers.
    const result = validateManifest(
      manifest({
        schemaVersion: 99,
        remote: { kind: 'federated', url: 'http://127.0.0.1:1/nope.js', module: './x' },
      }),
    )

    expect(result.code).toBe('unsupported-schema-version')
  })
})

describe('BR-AS06 — stable identity and order', () => {
  it('qualifies every contribution id with the plugin id', () => {
    const result = validateManifest(manifest())

    expect(result.plugin.contributions[0].qualifiedId).toBe('example-plugin/vessels')
  })

  it('lets two plugins declare the same local contribution id without collision', () => {
    const a = validateManifest(manifest({ id: 'fleet-ops', contributions: [
      { kind: 'route', id: 'overview', path: '/fleet-ops/overview', title: 'Overview' },
    ] }))
    const b = validateManifest(manifest({ id: 'port-insights', contributions: [
      { kind: 'route', id: 'overview', path: '/port-insights/overview', title: 'Overview' },
    ] }))

    expect(a.plugin.contributions[0].qualifiedId).not.toBe(b.plugin.contributions[0].qualifiedId)
  })

  it('rejects the same contribution id declared twice by one plugin', () => {
    const result = validateManifest(manifest({ contributions: [
      { kind: 'route', id: 'vessels', path: '/example-plugin/vessels', title: 'Vessels' },
      { kind: 'route', id: 'vessels', path: '/example-plugin/other', title: 'Other' },
    ] }))

    expect(result.ok).toBe(false)
    expect(result.code).toBe('duplicate-id')
  })

  it('rejects an id that is not kebab-case', () => {
    expect(validateManifest(manifest({ id: 'Example_Plugin' })).code).toBe('invalid-id')
  })

  it('orders contributions by order, then by declaration index', () => {
    const result = validateManifest(manifest({ contributions: [
      { kind: 'shell-footer', id: 'c', order: 10 },
      { kind: 'shell-footer', id: 'a', order: 1 },
      { kind: 'shell-footer', id: 'b', order: 1 },
    ] }))

    expect(result.plugin.contributions.map((c) => c.id)).toEqual(['a', 'b', 'c'])
  })

  it('is deterministic for contributions that declare no order at all', () => {
    const ids = () => validateManifest(manifest({ contributions: [
      { kind: 'shell-footer', id: 'z' },
      { kind: 'shell-footer', id: 'y' },
      { kind: 'shell-footer', id: 'x' },
    ] })).plugin.contributions.map((c) => c.id)

    expect(ids()).toEqual(['z', 'y', 'x'])
    expect(ids()).toEqual(ids())
  })
})

describe('BR-AS12 — addressable feature routes', () => {
  it('accepts a route under the plugin id', () => {
    expect(validateManifest(manifest()).ok).toBe(true)
  })

  it('accepts the plugin root as a route', () => {
    const result = validateManifest(manifest({ contributions: [
      { kind: 'route', id: 'home', path: '/example-plugin', title: 'Home' },
    ] }))

    expect(result.ok).toBe(true)
  })

  it('rejects a route outside the plugin namespace', () => {
    const result = validateManifest(manifest({ contributions: [
      { kind: 'route', id: 'sneaky', path: '/demos', title: 'Demos' },
    ] }))

    expect(result.ok).toBe(false)
    expect(result.code).toBe('route-not-namespaced')
  })

  it('namespaces routes under a declared routePrefix instead of the plugin id', () => {
    const result = validateManifest(manifest({
      id: 'demo-catalog',
      routePrefix: 'demos',
      contributions: [{ kind: 'route', id: 'catalog', path: '/demos', title: 'Demos' }],
    }))

    expect(result.ok).toBe(true)
    expect(result.plugin.routePrefix).toBe('demos')
  })

  it('still refuses a route outside the declared prefix', () => {
    const result = validateManifest(manifest({
      id: 'demo-catalog',
      routePrefix: 'demos',
      contributions: [{ kind: 'route', id: 'x', path: '/demo-catalog/x', title: 'X' }],
    }))

    expect(result.code).toBe('route-not-namespaced')
  })

  it('rejects a route prefix that is not kebab-case', () => {
    expect(validateManifest(manifest({ routePrefix: 'Demos/Extra' })).code).toBe('invalid-id')
  })

  it('defaults the prefix to the plugin id', () => {
    expect(validateManifest(manifest()).plugin.routePrefix).toBe('example-plugin')
  })

  it('rejects a prefix that only looks like the plugin namespace', () => {
    // '/example-plugin-evil' starts with the plugin id as a string but is a
    // different path segment, so it belongs to nobody.
    const result = validateManifest(manifest({ contributions: [
      { kind: 'route', id: 'evil', path: '/example-plugin-evil/x', title: 'Evil' },
    ] }))

    expect(result.code).toBe('route-not-namespaced')
  })
})

describe('BR-AS03 — independent deployment', () => {
  it('treats a plugin with no enabled flag as enabled', () => {
    expect(validateManifest(manifest()).plugin.enabled).toBe(true)
  })

  it('carries an operator disable through from the registry document', () => {
    expect(validateManifest(manifest({ enabled: false })).plugin.enabled).toBe(false)
  })
})

describe('extension point ids', () => {
  it('parses owner, region and major version', () => {
    expect(parseExtensionPointId('shell/topbar-controls/v1')).toEqual({
      owner: 'shell',
      region: 'topbar-controls',
      major: 1,
    })
  })

  it('parses a point owned by something other than the shell', () => {
    expect(parseExtensionPointId('demo-catalog/details-sidebar/v2').owner).toBe('demo-catalog')
  })

  it('rejects an id with no version', () => {
    expect(parseExtensionPointId('shell/topbar-controls')).toBeNull()
  })

  it('rejects a target that is not an extension-point id', () => {
    const result = validateManifest(manifest({ contributions: [
      { kind: 'extension', id: 'panel', target: 'anywhere' },
    ] }))

    expect(result.ok).toBe(false)
  })
})

describe('manifest hygiene', () => {
  it('rejects a plugin that contributes nothing', () => {
    expect(validateManifest(manifest({ contributions: [] })).ok).toBe(false)
  })

  it('rejects a remote kind the loader has no adapter for', () => {
    const result = validateManifest(manifest({ remote: { kind: 'iframe', url: 'x', module: 'y' } }))

    expect(result.ok).toBe(false)
    expect(result.code).toBe('malformed')
  })

  it('accepts a builtin remote, which needs no url', () => {
    const result = validateManifest(manifest({ remote: { kind: 'builtin', module: 'demo-catalog' } }))

    expect(result.ok).toBe(true)
    expect(result.plugin.remote.kind).toBe('builtin')
  })

  it('freezes the normalized plugin so indexing cannot mutate it', () => {
    const { plugin } = validateManifest(manifest())

    expect(Object.isFrozen(plugin)).toBe(true)
    expect(Object.isFrozen(plugin.contributions)).toBe(true)
  })
})

describe('BR-AS02 — a plugin may own extension points', () => {
  const withPoints = (points) =>
    validateManifest({
      ...manifest({
        id: 'demo-catalog',
        routePrefix: 'demos',
        contributions: [{ kind: 'route', id: 'catalog', path: '/demos', title: 'Demos' }],
      }),
      extensionPoints: points,
    })

  it('accepts a point in the plugin’s own namespace', () => {
    const result = withPoints([{ id: 'demo-catalog/details-sidebar/v1', capacity: 2 }])

    expect(result.ok).toBe(true)
    expect(result.plugin.extensionPoints).toEqual([
      { id: 'demo-catalog/details-sidebar/v1', capacity: 2, description: '' },
    ])
  })

  it('refuses a point owned by someone else', () => {
    // Otherwise a plugin could open `shell/topbar-controls/v1` itself and
    // capture contributions aimed at the host's region.
    const result = withPoints([{ id: 'shell/topbar-controls/v1' }])

    expect(result.ok).toBe(false)
    expect(result.code).toBe('extension-point-not-owned')
  })

  it('refuses an id that is not {owner}/{region}/v{major}', () => {
    expect(withPoints([{ id: 'details-sidebar' }]).code).toBe('malformed')
  })

  it('defaults an undeclared capacity to unbounded', () => {
    const result = withPoints([{ id: 'demo-catalog/details-sidebar/v1' }])

    expect(result.plugin.extensionPoints[0].capacity).toBe(Infinity)
  })

  it('declares none when the manifest omits the field', () => {
    expect(validateManifest(manifest()).plugin.extensionPoints).toEqual([])
  })
})

describe('federated container name (recorded revision of the 1a remote contract)', () => {
  // Phase 1b needed one field 1a did not have: Module Federation addresses a
  // container by a JS-identifier-shaped global name, while a plugin id is
  // kebab-case because it lands in URLs and store keys. Rather than mangle one
  // into the other by convention, the manifest may carry both, and the id
  // remains the identity everywhere above the loader.
  const federated = (remote) => validateManifest(manifest({ remote }))

  it('defaults the container name to the plugin id, made identifier-safe', () => {
    const result = federated({ kind: 'federated', url: 'http://localhost:7110/remoteEntry.js', module: 'plugin' })

    expect(result.ok).toBe(true)
    expect(result.plugin.remote.name).toBe('example_plugin')
  })

  it('carries an explicit container name through unchanged', () => {
    const result = federated({
      kind: 'federated',
      url: 'http://localhost:7110/remoteEntry.js',
      module: 'plugin',
      name: 'example_plugin',
    })

    expect(result.plugin.remote.name).toBe('example_plugin')
  })

  it('rejects a container name that is not a legal identifier', () => {
    // The name is interpolated into a federation module specifier and, in some
    // builds, a global. A registry entry is operator-supplied data, so it is
    // validated rather than trusted.
    for (const name of ['has space', '1leading-digit', 'semi;colon', '', 42]) {
      const result = federated({
        kind: 'federated',
        url: 'http://localhost:7110/remoteEntry.js',
        module: 'plugin',
        name,
      })
      expect(result.ok).toBe(false)
      expect(result.code).toBe('malformed')
    }
  })

  it('freezes the remote, name included', () => {
    const { plugin } = federated({
      kind: 'federated',
      url: 'http://localhost:7110/remoteEntry.js',
      module: 'plugin',
      name: 'example_plugin',
    })

    expect(Object.isFrozen(plugin.remote)).toBe(true)
  })
})
