import { describe, expect, it } from 'vitest'

import { validateManifest } from './manifestSchema.js'
import demoCatalogManifest from '../../../plugins/demo-catalog/public/manifest.json'

describe('BR-AS15 — the demo catalog is a plugin, not a special case', () => {
  it('passes the same manifest gate every other plugin passes', () => {
    const result = validateManifest(demoCatalogManifest)

    expect(result.ok).toBe(true)
  })

  it('contributes only through the public kinds', () => {
    const { plugin } = validateManifest(demoCatalogManifest)

    expect(new Set(plugin.contributions.map((c) => c.kind))).toEqual(new Set(['route', 'navigation']))
  })

  it('namespaces its routes under /demos', () => {
    const { plugin } = validateManifest(demoCatalogManifest)
    const routes = plugin.contributions.filter((c) => c.kind === 'route')

    expect(routes.map((r) => r.path)).toEqual(['/demos', '/demos/:id'])
  })

  it('owns the details-sidebar region other plugins target', () => {
    const { plugin } = validateManifest(demoCatalogManifest)

    expect(plugin.extensionPoints.map((p) => p.id)).toEqual(['demo-catalog/details-sidebar/v1'])
  })

  it('uses its independently served curated URL', () => {
    const { plugin } = validateManifest(demoCatalogManifest)

    expect(plugin.remote).toEqual({ kind: 'federated', name: 'demo_catalog', url: 'http://localhost:7112/remoteEntry.js', module: 'plugin' })
  })

  it('exports a component for every component name its routes declare', async () => {
    const module = await import('../../../plugins/demo-catalog/src/plugin.js')
    const { plugin } = validateManifest(demoCatalogManifest)

    for (const route of plugin.contributions.filter((c) => c.kind === 'route')) {
      expect(module.components[route.component]).toBeTruthy()
    }
  })
})
