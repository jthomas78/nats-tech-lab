import { describe, expect, it, vi } from 'vitest'

import { validateManifest } from '../registry/manifestSchema.js'
import { PLUGIN_STATUS, PluginStatusRecord } from '../registry/pluginStatus.js'
import { RemoteAllowlist } from '../registry/remoteAllowlist.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { createPluginLoader } from './pluginLoader.js'

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
    adapters: { federated: adapter },
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

  /* Rewritten at 8d (decision 92). This spec used to assert that activate()
     was passed nothing at all, on the argument that loading a plugin must not
     grant it access to the shell. Decision 90 hands over a shell-authored
     object instead — which is not the shell's internals, but does make the old
     wording false. What survives is the part that mattered: a plugin reaches
     exactly what the shell chose to put in the object, and nothing else. That
     is now decision 91's surface, asserted below. */
  it('passes activate() the shell API and nothing else', async () => {
    const activate = vi.fn()
    const { loader, plugin: p } = harness({ adapter: { load: async () => ({ activate }) } })

    await loader.load(p)

    expect(activate).toHaveBeenCalledTimes(1)
    const [api, ...rest] = activate.mock.calls[0]
    expect(rest).toEqual([])
    expect(api).toBeTypeOf('object')
  })

  it('activates a plugin that ignores the argument, unchanged', async () => {
    // The contract is additive: every plugin written before decision 90 takes
    // no parameter, and must behave exactly as it did.
    const activate = vi.fn(function noArgs() {})
    const { loader, plugin: p, record } = harness({
      adapter: { load: async () => ({ activate }) },
    })

    await loader.load(p)

    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
  })
})

describe('decision 91 — the v1 shell API surface is narrow and fixed', () => {
  const apiFrom = async () => {
    let seen = null
    const activate = vi.fn((api) => { seen = api })
    const { loader, plugin: p } = harness({ adapter: { load: async () => ({ activate }) } })
    await loader.load(p)
    return seen
  }

  it('shares one immutable API across plugins without freezing the Vue component', async () => {
    const first = await apiFrom()
    expect(await apiFrom()).toBe(first)
    expect(Object.isFrozen(first.ui)).toBe(true)
    expect(Object.isFrozen(first.ui.ExtensionRegion)).toBe(false)
  })

  it('carries exactly version and ui, so a widened surface fails here first', async () => {
    // Pinned rather than sampled. A surface can be widened by a decision and
    // never by an accident, and once a plugin depends on a field it cannot be
    // taken back out — so an unreviewed addition has to break a spec.
    expect(Object.keys(await apiFrom()).sort()).toEqual(['ui', 'version'])
  })

  it('reports the shell API version the manifest gate already enforces', async () => {
    // Same number manifestSchema.js checks shellApiVersion against (BR-AS13),
    // so a plugin can assert on it rather than guessing.
    expect((await apiFrom()).version).toBe(SHELL_API_VERSION)
  })

  it('exposes ExtensionRegion, and nothing else, under ui', async () => {
    const { ui } = await apiFrom()
    expect(Object.keys(ui)).toEqual(['ExtensionRegion'])
    expect(ui.ExtensionRegion).toBeTruthy()
  })

  it('withholds the shell connection, which decision 91 excluded from v1', async () => {
    // Explicit because the omission is the decision: the connection carries
    // the user's credential, and admitting it to v1 would be irreversible.
    const api = await apiFrom()
    expect(api.connection).toBeUndefined()
    expect(api.nats).toBeUndefined()
  })

  it('hands every plugin the same object, so one cannot mutate another\'s', async () => {
    expect(Object.isFrozen(await apiFrom())).toBe(true)
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
