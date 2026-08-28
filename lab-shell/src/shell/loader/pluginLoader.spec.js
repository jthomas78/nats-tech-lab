import { describe, expect, it, vi } from 'vitest'

import { validateManifest } from '../registry/manifestSchema.js'
import { PLUGIN_STATUS, PluginStatusRecord } from '../registry/pluginStatus.js'
import { RemoteAllowlist } from '../registry/registryClient.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { createBuiltinAdapter, createPluginLoader } from './pluginLoader.js'

const REMOTE_URL = 'http://localhost:7110/remoteEntry.js'

const plugin = (overrides = {}) =>
  validateManifest({
    id: 'example-plugin',
    name: 'Example Plugin',
    schemaVersion: REGISTRY_SCHEMA_VERSION,
    shellApiVersion: SHELL_API_VERSION,
    remote: { kind: 'federated', url: REMOTE_URL, module: './plugin' },
    contributions: [
      { kind: 'route', id: 'vessels', path: '/example-plugin/vessels', title: 'Vessels' },
    ],
    ...overrides,
  }).plugin

const harness = ({ adapter, curated = true, plugin: p = plugin() } = {}) => {
  const allowlist = new RemoteAllowlist()
  if (curated) allowlist.add(p)
  const statuses = new Map([[p.id, new PluginStatusRecord(p.id)]])
  statuses.get(p.id).transition(PLUGIN_STATUS.AVAILABLE)
  const loader = createPluginLoader({
    allowlist,
    adapters: { federated: adapter, builtin: adapter },
    statuses,
  })
  return { loader, statuses, plugin: p, record: statuses.get(p.id) }
}

describe('BR-AS01 — the loader fetches only curated remotes', () => {
  it('refuses a remote that is not in the registry document, without calling the adapter', async () => {
    const adapter = { load: vi.fn(async () => ({})) }
    const { loader, plugin: p, record } = harness({ adapter, curated: false })

    await expect(loader.load(p)).rejects.toThrow(/not in the curated plugin registry/)
    expect(adapter.load).not.toHaveBeenCalled()
    expect(record.reasonCode).toBe('remote-not-curated')
  })

  it('checks the allowlist even when the record came from the registry itself', async () => {
    // The guard is a property of the loader, not an argument about who calls
    // it — a later caller with a hand-built record must hit the same wall.
    const adapter = { load: vi.fn(async () => ({})) }
    const { loader } = harness({ adapter, curated: false })
    const forged = plugin({ remote: { kind: 'federated', url: 'http://evil.example/x.js', module: './x' } })

    await expect(loader.load(forged)).rejects.toThrow()
    expect(adapter.load).not.toHaveBeenCalled()
  })

  it('needs no allowlist entry for a builtin, which is in the shell bundle already', async () => {
    const p = plugin({ remote: { kind: 'builtin', module: 'demo-catalog' } })
    const adapter = createBuiltinAdapter({ 'demo-catalog': { activate() {} } })
    const { loader } = harness({ adapter, curated: false, plugin: p })

    await expect(loader.load(p)).resolves.toBeTruthy()
  })
})

describe('BR-AS08 — code loads once, on demand', () => {
  it('does not fetch anything until load is called', () => {
    const adapter = { load: vi.fn(async () => ({})) }
    harness({ adapter })

    expect(adapter.load).not.toHaveBeenCalled()
  })

  it('activates exactly once across repeated loads', async () => {
    const activate = vi.fn()
    const adapter = { load: vi.fn(async () => ({ activate })) }
    const { loader, plugin: p } = harness({ adapter })

    await loader.load(p)
    await loader.load(p)
    await loader.load(p)

    expect(adapter.load).toHaveBeenCalledTimes(1)
    expect(activate).toHaveBeenCalledTimes(1)
  })

  it('shares one load between callers that ask in the same tick', async () => {
    const activate = vi.fn()
    const adapter = { load: vi.fn(async () => ({ activate })) }
    const { loader, plugin: p } = harness({ adapter })

    await Promise.all([loader.load(p), loader.load(p)])

    expect(adapter.load).toHaveBeenCalledTimes(1)
    expect(activate).toHaveBeenCalledTimes(1)
  })

  it('walks the status machine through loading to active', async () => {
    const { loader, plugin: p, record } = harness({ adapter: { load: async () => ({}) } })

    await loader.load(p)

    expect(record.history).toEqual(['discovered', 'available', 'loading', 'active'])
  })

  it('reports loaded state without triggering a load', async () => {
    const adapter = { load: vi.fn(async () => ({})) }
    const { loader, plugin: p } = harness({ adapter })

    expect(loader.isLoaded(p.id)).toBe(false)
    expect(loader.peek(p.id)).toBeNull()
    expect(adapter.load).not.toHaveBeenCalled()

    await loader.load(p)
    expect(loader.isLoaded(p.id)).toBe(true)
  })

  it('passes the shell nothing to activate(), so loading grants no access', async () => {
    const activate = vi.fn()
    const { loader, plugin: p } = harness({ adapter: { load: async () => ({ activate }) } })

    await loader.load(p)

    expect(activate).toHaveBeenCalledWith()
  })
})

describe('BR-AS04 — a load failure is recorded, isolated, and retryable', () => {
  it('records a fetch failure as failed with a chunk code', async () => {
    const adapter = { load: async () => { throw new Error('Failed to fetch dynamically imported module') } }
    const { loader, plugin: p, record } = harness({ adapter })

    await expect(loader.load(p)).rejects.toThrow(/Failed to fetch/)
    expect(record.status).toBe(PLUGIN_STATUS.FAILED)
    expect(record.reasonCode).toBe('chunk-load-failed')
  })

  it('distinguishes a throwing activate() from a failed fetch', async () => {
    const adapter = { load: async () => ({ activate: () => { throw new Error('boom') } }) }
    const { loader, plugin: p, record } = harness({ adapter })

    await expect(loader.load(p)).rejects.toThrow('boom')
    expect(record.reasonCode).toBe('activate-threw')
  })

  it('does not cache a module whose activate() threw', async () => {
    const adapter = { load: async () => ({ activate: () => { throw new Error('boom') } }) }
    const { loader, plugin: p } = harness({ adapter })

    await expect(loader.load(p)).rejects.toThrow()

    expect(loader.isLoaded(p.id)).toBe(false)
  })

  it('retries a failed plugin, going back through loading', async () => {
    let attempt = 0
    const adapter = {
      load: async () => {
        attempt += 1
        if (attempt === 1) throw new Error('transient')
        return { activate: vi.fn() }
      },
    }
    const { loader, plugin: p, record } = harness({ adapter })

    await expect(loader.load(p)).rejects.toThrow('transient')
    await expect(loader.load(p)).resolves.toBeTruthy()

    expect(record.history).toEqual([
      'discovered', 'available', 'loading', 'failed', 'loading', 'active',
    ])
  })

  it('refuses to load a plugin the registry never made available', async () => {
    const p = plugin()
    const statuses = new Map([[p.id, new PluginStatusRecord(p.id)]])
    statuses.get(p.id).transition(PLUGIN_STATUS.INCOMPATIBLE)
    const loader = createPluginLoader({
      allowlist: new RemoteAllowlist().add(p),
      adapters: { federated: { load: vi.fn() } },
      statuses,
    })

    await expect(loader.load(p)).rejects.toThrow(/cannot be loaded from status incompatible/)
  })

  it('rejects a module that is not a module', async () => {
    const { loader, plugin: p, record } = harness({ adapter: { load: async () => undefined } })

    await expect(loader.load(p)).rejects.toThrow(/exported no module/)
    expect(record.reasonCode).toBe('malformed-module')
  })
})

describe('the builtin adapter', () => {
  it('resolves a module the shell bundles', async () => {
    const module = { activate() {} }
    const adapter = createBuiltinAdapter({ 'demo-catalog': module })

    await expect(adapter.load({ module: 'demo-catalog' })).resolves.toBe(module)
  })

  it('accepts a factory, so a built-in can still be code-split', async () => {
    const adapter = createBuiltinAdapter({ 'demo-catalog': () => ({ activate() {} }) })

    await expect(adapter.load({ module: 'demo-catalog' })).resolves.toHaveProperty('activate')
  })

  it('throws for a module it does not bundle', async () => {
    const adapter = createBuiltinAdapter({})

    await expect(adapter.load({ module: 'nope' })).rejects.toThrow(/No built-in module/)
  })
})
