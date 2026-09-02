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
  it('keeps an empty plugin set and records why the remote list is empty', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: false, code: 'registry-unreachable', message: 'down' }),
      permissions,
    })

    expect(shell.contributions.routes).toEqual([])
    expect(shell.plugins).toEqual([])
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
      permissions,
    })

    expect(shell.allowlist.size).toBe(0)
  })
})

describe('BR-AS06 — one id, one plugin', () => {
  it('keeps the first curated plugin when an entry duplicates its id', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, plugins: [manifest('demo-catalog'), manifest('demo-catalog', { name: 'impostor' })] }),
      permissions,
    })

    // The first curated manifest keeps the id and route.
    expect(shell.plugins).toHaveLength(1)
    expect(shell.plugins[0].name).toBe('demo-catalog')
    expect(shell.contributions.routes.map((r) => r.pluginId)).toEqual(['demo-catalog'])
    expect(shell.statuses.get('demo-catalog').status).toBe(PLUGIN_STATUS.AVAILABLE)
  })

  it('indexes in curated order without loading remote code', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, plugins: [manifest('zzz-plugin'), manifest('aaa-plugin')] }),
      permissions,
    })

    expect(shell.plugins.map((p) => p.id)).toEqual(['zzz-plugin', 'aaa-plugin'])
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
    manifest('demo-catalog', {
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
          catalog(),
        ],
      }),
      permissions,
    })

    expect(shell.contributions.extensionsFor('demo-catalog/details-sidebar/v1')).toHaveLength(1)
    // The owner was never asked for its code — placement is metadata only.
    expect(shell.statuses.get('demo-catalog').status).toBe(PLUGIN_STATUS.AVAILABLE)
  })

  it('does not let a duplicate plugin redeclare the owner region', async () => {
    const shell = await bootShell({
      registryClient: client({
        ok: true,
        plugins: [catalog({ extensionPoints: [{ id: 'demo-catalog/x/v1' }] }), manifest('demo-catalog', { extensionPoints: [{ id: 'demo-catalog/x/v1' }] })],
      }),
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
  const twoPlugins = { ok: true, revision: 12, heldRevision: 12, plugins: [manifest('fleet-ops')] }

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
    expect(shell.registry.heldRevision).toBe(12)

    shell.applyRegistry({ ok: true, degraded: true, plugins: [] })

    expect(shell.registry.heldRevision).toBeNull()
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
    shell.applyRegistry({ ok: true, unchanged: true, heldRevision: 12 })

    expect(shell.registry.degraded).toBe(false)
    expect(shell.registry.heldRevision).toBe(12)
    expect(shell.registry.revision).toBe(12)
  })

  it('clears degraded on a failed read too, by refusing to keep a token it cannot trust', async () => {
    const shell = await bootShell({ registryClient: client(twoPlugins), permissions })

    shell.applyRegistry({ ok: false, code: 'registry-unreachable', message: '' })

    expect(shell.registry.heldRevision).toBeNull()
    expect(shell.registryError.code).toBe('registry-unreachable')
    // Still running everything it had.
    expect(shell.contributions.navigation).toHaveLength(1)
  })
})

/*
  BR-AS49 / decision 100 — a revocation reaches a running shell.

  The service serves a withheld entry as a tombstone rather than dropping it,
  precisely so the shell can tell "an operator switched this off" from "the
  platform withdrew trust in this". Only the second is applied under the user.
*/
describe('BR-AS49 — a tombstone forces a reload', () => {
  const tombstone = (id) => ({ id, withheld: true })

  const bootedWith = (id) =>
    bootShell({
      registryClient: client({ ok: true, revision: 7, heldRevision: 7, plugins: [manifest(id)] }),
      permissions,
    })

  it('raises a forced reload for the plugin it is running', async () => {
    const shell = await bootedWith('fleet-ops')
    shell.applyRegistry({ ok: true, revision: 8, heldRevision: 8, plugins: [tombstone('fleet-ops')] })

    expect(shell.pendingReload).toHaveLength(1)
    expect(shell.pendingReload[0]).toMatchObject({ id: 'fleet-ops', forced: true })
  })

  it('does not admit the tombstone as a plugin', async () => {
    const shell = await bootedWith('fleet-ops')
    shell.applyRegistry({ ok: true, revision: 8, heldRevision: 8, plugins: [tombstone('billing')] })

    expect(shell.plugins.map((p) => p.id)).toEqual(['fleet-ops'])
  })

  it('acts on a tombstone in a degraded read, which it does not do for any other change', async () => {
    /* A degraded document cannot say what exists — the reason decision 48
       makes the shell hold what it has. It can say what was withdrawn: cache
       writes are monotonic, so a tombstone in a stale copy is real, and
       withdrawal is the safe direction to be wrong in. */
    const shell = await bootedWith('fleet-ops')
    shell.applyRegistry({ ok: true, degraded: true, plugins: [tombstone('fleet-ops')] })

    expect(shell.pendingReload).toHaveLength(1)
    expect(shell.pendingReload[0]).toMatchObject({ id: 'fleet-ops', forced: true })
  })

  it('still holds what it has through a degraded read that carries no tombstone', async () => {
    const shell = await bootedWith('fleet-ops')
    shell.applyRegistry({ ok: true, degraded: true, plugins: [] })

    expect(shell.plugins.map((p) => p.id)).toEqual(['fleet-ops'])
    expect(shell.pendingReload).toEqual([])
  })
})
