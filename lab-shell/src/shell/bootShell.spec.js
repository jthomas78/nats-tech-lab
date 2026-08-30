import { watchSyncEffect } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import { createPermissionEvaluator } from './auth/permissions.js'
import { PLUGIN_STATUS } from './registry/pluginStatus.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from './versions.js'
import { bootShell, withRuntime } from './bootShell.js'

const manifest = (id, overrides = {}) => ({
  id,
  name: id,
  schemaVersion: REGISTRY_SCHEMA_VERSION,
  shellApiVersion: SHELL_API_VERSION,
  remote: { kind: 'federated', url: `http://localhost:7110/${id}.js`, module: './plugin' },
  contributions: [
    { kind: 'route', id: 'home', path: `/${id}`, title: id },
    { kind: 'navigation', id: 'nav', label: id, route: 'home' },
  ],
  ...overrides,
})

const builtin = (id, overrides = {}) =>
  manifest(id, { remote: { kind: 'builtin', module: id }, ...overrides })

const client = (result) => ({ fetchRegistry: vi.fn(async () => result) })
const permissions = createPermissionEvaluator({ permissions: ['*'] })

describe('BR-AS08 — a booted shell has placed everything and loaded nothing', () => {
  it('indexes routes and nav without fetching any remote entry', async () => {
    const fetchSpy = vi.fn()
    const shell = await bootShell({
      registryClient: client({ ok: true, plugins: [manifest('fleet-ops')] }),
      permissions,
    })

    expect(shell.contributions.routes).toHaveLength(1)
    expect(shell.contributions.navigation).toHaveLength(1)
    expect(shell.statuses.get('fleet-ops').status).toBe(PLUGIN_STATUS.AVAILABLE)
    expect(fetchSpy).not.toHaveBeenCalled()
  })
})

describe('BR-AS04 — the shell boots when the registry does not answer', () => {
  it('keeps its built-ins and records why the remote list is empty', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: false, code: 'registry-unreachable', message: 'down' }),
      builtins: [builtin('demo-catalog', {
        routePrefix: 'demos',
        contributions: [{ kind: 'route', id: 'catalog', path: '/demos', title: 'Demos' }],
      })],
      permissions,
    })

    expect(shell.contributions.routes.map((r) => r.path)).toEqual(['/demos'])
    expect(shell.registryError.code).toBe('registry-unreachable')
  })

  it('does not throw when the registry is down', async () => {
    await expect(
      bootShell({
        registryClient: client({ ok: false, code: 'registry-http-503', message: 'x' }),
        permissions,
      }),
    ).resolves.toBeTruthy()
  })

  it('keeps the good plugins when one manifest in the document is unreadable', async () => {
    const shell = await bootShell({
      registryClient: client({
        ok: true,
        plugins: [manifest('broken-plugin', { schemaVersion: 99 }), manifest('fleet-ops')],
      }),
      permissions,
    })

    expect(shell.statuses.get('broken-plugin').status).toBe(PLUGIN_STATUS.INCOMPATIBLE)
    expect(shell.statuses.get('fleet-ops').status).toBe(PLUGIN_STATUS.AVAILABLE)
    expect(shell.contributions.routes).toHaveLength(1)
  })
})

describe('BR-AS01 — only curated remotes reach the allowlist', () => {
  it('allows a remote from the document', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, plugins: [manifest('fleet-ops')] }),
      permissions,
    })

    expect(shell.allowlist.allows('http://localhost:7110/fleet-ops.js')).toBe(true)
  })

  it('allows nothing when the document could not be read', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: false, code: 'registry-unreachable', message: 'down' }),
      builtins: [builtin('demo-catalog', {
        routePrefix: 'demos',
        contributions: [{ kind: 'route', id: 'catalog', path: '/demos', title: 'Demos' }],
      })],
      permissions,
    })

    expect(shell.allowlist.size).toBe(0)
  })

  it('refuses a built-in that claims a federated remote', async () => {
    // A built-in is trusted because it is in the shell's bundle. One that
    // names a URL is asking for that trust while fetching from elsewhere.
    const shell = await bootShell({
      registryClient: client({ ok: true, plugins: [] }),
      builtins: [manifest('smuggler')],
      permissions,
    })

    expect(shell.statuses.get('smuggler').reasonCode).toBe('builtin-must-be-bundled')
    expect(shell.allowlist.size).toBe(0)
  })
})

describe('BR-AS06 — one id, one plugin', () => {
  it('refuses a remote plugin that reuses a built-in id', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, plugins: [manifest('demo-catalog')] }),
      builtins: [builtin('demo-catalog', {
        routePrefix: 'demos',
        contributions: [{ kind: 'route', id: 'catalog', path: '/demos', title: 'Demos' }],
      })],
      permissions,
    })

    // The built-in keeps the id and the route; the impostor is reported.
    expect(shell.contributions.routes.map((r) => r.pluginId)).toEqual(['demo-catalog'])
    expect(shell.statuses.get('demo-catalog').status).toBe(PLUGIN_STATUS.AVAILABLE)
  })

  it('indexes built-ins before remotes, so ordering does not depend on the network', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, plugins: [manifest('aaa-plugin')] }),
      builtins: [builtin('zzz-builtin')],
      permissions,
    })

    expect(shell.plugins.map((p) => p.id)).toEqual(['zzz-builtin', 'aaa-plugin'])
  })
})

describe('the Plugins screen inventory', () => {
  it('reports a row per plugin with its status and its refusals', async () => {
    const shell = await bootShell({
      registryClient: client({
        ok: true,
        plugins: [
          manifest('fleet-ops', {
            contributions: [
              { kind: 'route', id: 'home', path: '/fleet-ops', title: 'Fleet' },
              { kind: 'extension', id: 'panel', target: 'shell/nowhere/v1' },
            ],
          }),
          manifest('old-plugin', { shellApiVersion: SHELL_API_VERSION + 1 }),
        ],
      }),
      permissions,
    })

    const rows = Object.fromEntries(shell.inventory.map((r) => [r.id, r]))
    expect(rows['fleet-ops'].status).toBe(PLUGIN_STATUS.AVAILABLE)
    expect(rows['fleet-ops'].refusals[0].code).toBe('unknown-extension-point')
    expect(rows['old-plugin'].status).toBe(PLUGIN_STATUS.INCOMPATIBLE)
    expect(rows['old-plugin'].reasonCode).toBe('unsupported-shell-api-version')
  })
})

describe('BR-AS02 — plugin-owned extension points open at boot', () => {
  const catalog = (overrides = {}) =>
    builtin('demo-catalog', {
      routePrefix: 'demos',
      contributions: [{ kind: 'route', id: 'catalog', path: '/demos', title: 'Demos' }],
      extensionPoints: [{ id: 'demo-catalog/details-sidebar/v1', capacity: 2 }],
      ...overrides,
    })

  it('places a contribution into a region whose owner has not been loaded', async () => {
    const shell = await bootShell({
      registryClient: client({
        ok: true,
        plugins: [
          manifest('fleet-ops', {
            contributions: [
              { kind: 'extension', id: 'panel', target: 'demo-catalog/details-sidebar/v1' },
            ],
          }),
        ],
      }),
      builtins: [catalog()],
      permissions,
    })

    expect(shell.contributions.extensionsFor('demo-catalog/details-sidebar/v1')).toHaveLength(1)
    // The owner was never asked for its code — placement is metadata only.
    expect(shell.statuses.get('demo-catalog').status).toBe(PLUGIN_STATUS.AVAILABLE)
  })

  it('refuses the second plugin to claim the same region', async () => {
    const shell = await bootShell({
      registryClient: client({
        ok: true,
        plugins: [manifest('demo-catalog', { extensionPoints: [{ id: 'demo-catalog/x/v1' }] })],
      }),
      builtins: [catalog({ extensionPoints: [{ id: 'demo-catalog/x/v1' }] })],
      permissions,
    })

    expect(shell.extensionPoints.has('demo-catalog/x/v1')).toBe(true)
    expect(shell.contributions.routes.map((r) => r.path)).toEqual(['/demos'])
  })
})

describe('the inventory tracks transitions that happen after boot', () => {
  /* A plugin only fails when something first asks for its code, which is long
     after boot. The Plugins screen is the one surface that reports it, so a
     status record whose later mutations nothing can observe would leave the
     table permanently stale — showing `available` beside a visibly broken
     feature. */
  it('re-renders a row when a plugin fails on first use', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, plugins: [manifest('fleet-ops')] }),
      permissions,
    })

    const seen = []
    watchSyncEffect(() => {
      seen.push(shell.inventory.find((row) => row.id === 'fleet-ops').status)
    })
    expect(seen).toEqual([PLUGIN_STATUS.AVAILABLE])

    shell.statuses.get('fleet-ops').transition(PLUGIN_STATUS.FAILED, {
      code: 'chunk-load-failed',
      message: 'unreachable',
    })

    /* `transition` writes several fields, so the sync effect runs more than
       once — what matters is that it ran at all and settled on the new
       status. */
    expect(seen.at(-1)).toBe(PLUGIN_STATUS.FAILED)
    expect(shell.inventory.find((row) => row.id === 'fleet-ops').reasonCode).toBe(
      'chunk-load-failed',
    )
  })

  it('keeps the inventory live through the composed object the app provides', async () => {
    const booted = await bootShell({
      registryClient: client({ ok: true, plugins: [manifest('fleet-ops')] }),
      permissions,
    })
    const shell = withRuntime(booted, { loader: {}, router: {} })

    shell.statuses.get('fleet-ops').transition(PLUGIN_STATUS.FAILED, {
      code: 'chunk-load-failed',
    })

    expect(shell.inventory.find((row) => row.id === 'fleet-ops').status).toBe(PLUGIN_STATUS.FAILED)
    expect(shell.loader).toBeDefined()
  })
})

describe('BR-AS22 / decision 48 — degraded is a state the shell leaves', () => {
  const twoPlugins = { ok: true, revision: 12, etag: '"12"', plugins: [manifest('fleet-ops')] }

  it('holds what it has and offers no reload when a read comes back degraded', async () => {
    const shell = await bootShell({ registryClient: client(twoPlugins), permissions })

    shell.applyRegistry({ ok: true, degraded: true, plugins: [] })

    expect(shell.registry.degraded).toBe(true)
    // An empty degraded document is not the claim "everything was withdrawn".
    expect(shell.pendingReload).toHaveLength(0)
    expect(shell.contributions.navigation).toHaveLength(1)
    // Held revision survives: the shell still knows what it is running.
    expect(shell.registry.revision).toBe(12)
  })

  it('drops the conditional token when it degrades, so recovery is unconditional', async () => {
    const shell = await bootShell({ registryClient: client(twoPlugins), permissions })
    expect(shell.registry.etag).toBe('"12"')

    shell.applyRegistry({ ok: true, degraded: true, plugins: [] })

    expect(shell.registry.etag).toBeNull()
  })

  it('recovers at the same revision — the case that answers 304', async () => {
    /* The one-way door, in the order it actually happened: degrade, then
       recover with nothing having changed server-side. Keeping the pre-outage
       ETag made the recovery read a 304, and the `unchanged` guard used to sit
       ABOVE the line that clears `degraded` — so the shell stayed degraded on
       screen until something else was published. */
    const shell = await bootShell({ registryClient: client(twoPlugins), permissions })
    shell.applyRegistry({ ok: true, degraded: true, plugins: [] })

    // Same revision, no document: exactly what a recovered service answers to
    // an unconditional read it can satisfy from cache, or to a 304.
    shell.applyRegistry({ ok: true, unchanged: true, etag: '"12"' })

    expect(shell.registry.degraded).toBe(false)
    expect(shell.registry.etag).toBe('"12"')
    expect(shell.registry.revision).toBe(12)
  })

  it('clears degraded on a failed read too, by refusing to keep a token it cannot trust', async () => {
    const shell = await bootShell({ registryClient: client(twoPlugins), permissions })

    shell.applyRegistry({ ok: false, code: 'registry-unreachable', message: '' })

    expect(shell.registry.etag).toBeNull()
    expect(shell.registryError.code).toBe('registry-unreachable')
    // Still running everything it had.
    expect(shell.contributions.navigation).toHaveLength(1)
  })
})
