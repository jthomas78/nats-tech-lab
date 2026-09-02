import { describe, expect, it, vi } from 'vitest'

import { validateManifest } from '../registry/manifestSchema.js'
import { PLUGIN_STATUS, PluginStatusRecord } from '../registry/pluginStatus.js'
import { RemoteAllowlist } from '../registry/remoteAllowlist.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { createPluginLoader } from './pluginLoader.js'

/*
  A withdrawal can land at any moment, including in the middle of an import
  (BR-AS56). The module is kept — the shell promises to stop showing the
  plugin, not to unload JavaScript — but a load that finishes after the
  withdrawal must not put the plugin back into `active`.
*/

const REMOTE_URL = 'http://localhost:7110/remoteEntry.js'

const plugin = () =>
  validateManifest({
    id: 'example-plugin',
    name: 'Example Plugin',
    schemaVersion: REGISTRY_SCHEMA_VERSION,
    shellApiVersion: SHELL_API_VERSION,
    remote: { kind: 'federated', url: REMOTE_URL, module: './plugin' },
    contributions: [
      { kind: 'route', id: 'vessels', path: '/example-plugin/vessels', title: 'Vessels' },
    ],
  }).plugin

const harness = (adapter) => {
  const p = plugin()
  const allowlist = new RemoteAllowlist()
  allowlist.add(p)
  const statuses = new Map([[p.id, new PluginStatusRecord(p.id)]])
  statuses.get(p.id).transition(PLUGIN_STATUS.AVAILABLE)
  const loader = createPluginLoader({ allowlist, adapters: { federated: adapter }, statuses })
  return { loader, plugin: p, record: statuses.get(p.id) }
}

describe('BR-AS56 — a load that finishes late cannot resurrect the plugin', () => {
  it('keeps the module but leaves the status withdrawn', async () => {
    let finish
    const activate = vi.fn()
    const adapter = { load: () => new Promise((resolve) => { finish = () => resolve({ activate }) }) }
    const { loader, plugin: p, record } = harness(adapter)

    const pending = loader.load(p)
    record.withdraw()
    finish()
    await pending

    expect(record.status).toBe(PLUGIN_STATUS.WITHDRAWN)
    expect(loader.isLoaded(p.id)).toBe(true)
  })

  it('sends an unchanged return straight to active, without a second activate()', async () => {
    const activate = vi.fn()
    const adapter = { load: async () => ({ activate }) }
    const { loader, plugin: p, record } = harness(adapter)

    await loader.load(p)
    record.withdraw()
    record.restore()
    await loader.load(p)

    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
    expect(activate).toHaveBeenCalledTimes(1)
  })

  it('refuses to start a load while the plugin is withdrawn', async () => {
    const adapter = { load: vi.fn(async () => ({})) }
    const { loader, plugin: p, record } = harness(adapter)

    record.withdraw()

    await expect(loader.load(p)).rejects.toThrow(/withdrawn/)
    expect(adapter.load).not.toHaveBeenCalled()
  })
})
